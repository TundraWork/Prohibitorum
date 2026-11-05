package session

import (
	"context"
	"errors"
	"net/http"
	"sort"
	"time"

	"prohibitorum/pkg/authn"
	"prohibitorum/pkg/configx"
	"prohibitorum/pkg/db"
)

// AccountHeader names the account a dashboard API request acts as (request)
// and the account a cookie session was selected for (response). The request
// header only applies under /api/prohibitorum/; protocol endpoints are full
// page navigations that cannot carry it.
const AccountHeader = "X-Prohibitorum-Account"

// DefaultMaxAccounts is the per-browser account cap when the config leaves it
// unset (tests that build a bare Config).
const DefaultMaxAccounts = 10

// AccountQueries is the account lookup a Browser needs to validate entries.
type AccountQueries interface {
	GetAccountByID(ctx context.Context, id int32) (db.Account, error)
}

// Manager reads and writes the accounts signed in on each browser.
type Manager struct {
	cfg   *configx.Config
	q     AccountQueries
	store *SessionStore
	ipOf  func(*http.Request) string
}

// NewManager builds a Manager. ipOf resolves the client IP recorded on
// sessions; nil records none.
func NewManager(cfg *configx.Config, q AccountQueries, store *SessionStore, ipOf func(*http.Request) string) *Manager {
	if ipOf == nil {
		ipOf = func(*http.Request) string { return "" }
	}
	return &Manager{cfg: cfg, q: q, store: store, ipOf: ipOf}
}

// Limit is the configured per-browser account cap.
func (m *Manager) Limit() int {
	if m.cfg == nil || m.cfg.SessionMaxAccounts <= 0 {
		return DefaultMaxAccounts
	}
	return m.cfg.SessionMaxAccounts
}

// Store returns the session store the manager issues into.
func (m *Manager) Store() *SessionStore { return m.store }

type browserCtxKey struct{}

// account loads an account row. A Manager built without queries (focused
// handler tests) knows accounts only by ID.
func (m *Manager) account(ctx context.Context, id int32) (db.Account, error) {
	if m.q == nil {
		return db.Account{ID: id}, nil
	}
	return m.q.GetAccountByID(ctx, id)
}

// BrowserFromContext returns the Browser LoadSession attached, or nil (PAT
// requests, and handlers run without the middleware).
func BrowserFromContext(ctx context.Context) *Browser {
	b, _ := ctx.Value(browserCtxKey{}).(*Browser)
	return b
}

// WithBrowser attaches b to ctx.
func WithBrowser(ctx context.Context, b *Browser) context.Context {
	return context.WithValue(ctx, browserCtxKey{}, b)
}

// BrowserFor returns the request's Browser, reading the cookies afresh when
// LoadSession did not run (handler tests, PAT requests).
func (m *Manager) BrowserFor(r *http.Request) *Browser {
	if b := BrowserFromContext(r.Context()); b != nil {
		return b
	}
	return m.newBrowser(r)
}

// entry states.
const (
	entryUnchecked = iota
	entryValid
	entryUnavailable // transient lookup failure: kept, not usable this request
	entryGone
)

type browserEntry struct {
	Entry
	state    int
	session  *authn.Session
	disabled *db.Account
	touched  bool
}

// Browser is the set of accounts signed in on one browser, read from its two
// session cookies. Entries are validated lazily and the result cached for the
// request: an entry whose session expired, whose account was deleted or
// disabled, is revoked and dropped, and the cookies are rewritten when the
// response is written.
//
// PHB-286 extends the fixed order (ordered) with linked accounts that have
// no session of their own; everything here works on accounts rather than
// cookie entries so that extension does not change callers.
type Browser struct {
	m       *Manager
	ctx     context.Context
	r       *http.Request
	entries []*browserEntry
	// dirtyPersistent / dirtyTemp mark which cookie must be rewritten.
	dirtyPersistent bool
	dirtyTemp       bool
	// disabledSeen is the account of a dropped disabled entry, so a request
	// that has no other account still answers account_disabled.
	disabledSeen *db.Account
}

func (m *Manager) newBrowser(r *http.Request) *Browser {
	b := &Browser{m: m, ctx: r.Context(), r: r}
	var all []Entry
	if c, err := r.Cookie(PersistentCookieName(m.cfg)); err == nil && c.Value != "" {
		got := DecodeCookie(c.Value)
		for i := range got {
			got[i].Remember = true
		}
		if len(got) == 0 {
			b.dirtyPersistent = true
		}
		all = append(all, got...)
	}
	if c, err := r.Cookie(TempCookieName(m.cfg)); err == nil && c.Value != "" {
		got := DecodeCookie(c.Value)
		// The old format only ever lived in the persistent cookie.
		kept := got[:0]
		for _, e := range got {
			if !e.legacy {
				kept = append(kept, e)
			}
		}
		if len(kept) != len(got) || len(kept) == 0 {
			b.dirtyTemp = true
		}
		all = append(all, kept...)
	}
	sort.SliceStable(all, func(i, j int) bool { return all[i].Seq < all[j].Seq })
	count := make(map[int32]int, len(all))
	for _, e := range all {
		count[e.AccountID]++
	}
	settled := make(map[int32]bool, len(all))
	for _, e := range all {
		if count[e.AccountID] > 1 {
			if settled[e.AccountID] {
				continue
			}
			settled[e.AccountID] = true
			b.entries = append(b.entries, b.pickDuplicate(all, e.AccountID))
			continue
		}
		b.entries = append(b.entries, &browserEntry{Entry: e})
	}
	return b
}

// pickDuplicate settles an account that appears in the cookies more than
// once. The earliest entry whose session is live is kept; only other live
// sessions are revoked. A cookie is not proof of anything — without the
// __Host- prefix a sibling subdomain can plant an entry — so an entry that
// does not check out must never get a real session revoked.
func (b *Browser) pickDuplicate(all []Entry, accountID int32) *browserEntry {
	var keep *browserEntry
	for _, e := range all {
		if e.AccountID != accountID {
			continue
		}
		b.markDirty(e.Remember)
		if _, err := b.m.store.Peek(b.ctx, e.AccountID, e.Token); err != nil {
			continue
		}
		if keep == nil {
			keep = &browserEntry{Entry: e}
			continue
		}
		_ = b.m.store.Revoke(b.ctx, e.AccountID, e.Token)
	}
	if keep == nil {
		// None is live: keep the first; checking it drops it.
		for _, e := range all {
			if e.AccountID == accountID {
				return &browserEntry{Entry: e}
			}
		}
	}
	return keep
}

func (b *Browser) markDirty(remember bool) {
	if remember {
		b.dirtyPersistent = true
	} else {
		b.dirtyTemp = true
	}
}

// Dirty reports whether the cookies need rewriting.
func (b *Browser) Dirty() bool { return b.dirtyPersistent || b.dirtyTemp }

func (b *Browser) check(e *browserEntry) {
	if e.state != entryUnchecked {
		return
	}
	ctx := b.ctx
	data, err := b.m.store.Peek(ctx, e.AccountID, e.Token)
	if err != nil {
		var ae *authn.AuthError
		if errors.As(err, &ae) && ae.Code == "no_session" {
			b.drop(e, false)
			return
		}
		e.state = entryUnavailable
		return
	}
	account, err := b.m.account(ctx, e.AccountID)
	if err != nil {
		// Account vanished (deleted while the session was live).
		b.drop(e, true)
		return
	}
	if account.Disabled {
		// Disabled mid-session: revoke and drop only this account's entry.
		b.drop(e, true)
		acct := account
		e.disabled = &acct
		if b.disabledSeen == nil {
			b.disabledSeen = &acct
		}
		return
	}
	if e.legacy {
		e.legacy = false
		e.Seq = data.IssuedAt.UnixMilli()
		if e.Seq <= 0 {
			e.Seq = 1
		}
		// Rewrite the old value in the new format, in the cookie the
		// session's own remember choice belongs to.
		b.dirtyPersistent = true
		e.Remember = data.Remember
		b.markDirty(e.Remember)
		b.resort()
	}
	e.state = entryValid
	e.session = &authn.Session{Account: &account, Token: e.Token, Data: data}
}

// Touch records activity on accountID's session: its idle expiry slides and
// LastSeenIP is updated. Only the account a request acts as is touched, so
// using one account never keeps another, idle one signed in.
func (b *Browser) Touch(accountID int32) {
	e := b.find(accountID)
	if e == nil {
		return
	}
	b.check(e)
	if e.state != entryValid || e.touched {
		return
	}
	e.touched = true
	data, refreshed, err := b.m.store.Load(b.ctx, e.AccountID, e.Token, b.m.ipOf(b.r), b.r.UserAgent())
	if err != nil {
		return
	}
	*e.session.Data = *data
	if refreshed && e.Remember {
		// Extend the persistent cookie's Max-Age with the sliding window;
		// the temp cookie has no Max-Age to extend.
		b.dirtyPersistent = true
	}
}

func (b *Browser) drop(e *browserEntry, revoke bool) {
	if revoke {
		_ = b.m.store.Revoke(b.ctx, e.AccountID, e.Token)
	}
	e.state = entryGone
	b.markDirty(e.Remember)
	if e.legacy {
		b.dirtyPersistent = true
	}
}

func (b *Browser) resort() {
	sort.SliceStable(b.entries, func(i, j int) bool { return b.entries[i].Seq < b.entries[j].Seq })
}

func (b *Browser) checkAll() {
	for _, e := range b.entries {
		b.check(e)
	}
	b.resort()
}

// ordered returns the valid entries in the fixed sign-in order.
func (b *Browser) ordered() []*browserEntry {
	b.checkAll()
	out := make([]*browserEntry, 0, len(b.entries))
	for _, e := range b.entries {
		if e.state == entryValid {
			out = append(out, e)
		}
	}
	return out
}

// Accounts returns every signed-in account's session in the fixed order.
func (b *Browser) Accounts() []*authn.Session {
	es := b.ordered()
	out := make([]*authn.Session, len(es))
	for i, e := range es {
		out[i] = e.session
	}
	return out
}

// AccountIDs returns the signed-in account IDs in the fixed order.
func (b *Browser) AccountIDs() []int32 {
	es := b.ordered()
	out := make([]int32, len(es))
	for i, e := range es {
		out[i] = e.AccountID
	}
	return out
}

// Count is the number of signed-in accounts.
func (b *Browser) Count() int { return len(b.ordered()) }

// held counts the entries still in the cookies, including ones whose session
// could not be read this request; the cap applies to all of them.
func (b *Browser) held() int {
	b.checkAll()
	n := 0
	for _, e := range b.entries {
		if e.state == entryValid || e.state == entryUnavailable {
			n++
		}
	}
	return n
}

// Unsettled reports whether an entry's session could not be read this
// request (a transient store failure). Accounts() then lacks an account that
// is still signed in, so a protocol flow must not pick "the only account".
func (b *Browser) Unsettled() bool {
	b.checkAll()
	for _, e := range b.entries {
		if e.state == entryUnavailable {
			return true
		}
	}
	return false
}

// HasAnyEntry reports whether the cookies name any account, without
// validating them; cheap enough for every page load.
func (b *Browser) HasAnyEntry() bool {
	for _, e := range b.entries {
		if e.state != entryGone {
			return true
		}
	}
	return false
}

// Default returns the first directly signed-in account in the fixed order,
// checking entries only until one is valid. When none is but a disabled
// account was dropped, it returns that account as a sentinel session (no
// token, no data) so authn.Check answers account_disabled.
func (b *Browser) Default() *authn.Session {
	b.resortUnchecked()
	for _, e := range b.entries {
		b.check(e)
		if e.state == entryValid {
			return e.session
		}
		if e.state == entryUnavailable {
			// The default account could not be read this time; acting as
			// the next account instead would switch identity silently.
			return nil
		}
	}
	if b.disabledSeen != nil {
		return &authn.Session{Account: b.disabledSeen}
	}
	return nil
}

// resortUnchecked keeps legacy entries (Seq unknown) first, where their age
// almost certainly puts them; Default checks in this order.
func (b *Browser) resortUnchecked() { b.resort() }

func (b *Browser) find(accountID int32) *browserEntry {
	for _, e := range b.entries {
		if e.AccountID == accountID && e.state != entryGone {
			return e
		}
		if e.AccountID == accountID && e.disabled != nil {
			return e
		}
	}
	return nil
}

// Select returns the session of accountID when it is signed in here. A
// disabled account answers with the sentinel session, as Default does.
// PHB-286 creates a session on demand for a linked account at the point this
// returns nil.
func (b *Browser) Select(accountID int32) *authn.Session {
	e := b.find(accountID)
	if e == nil {
		return nil
	}
	b.check(e)
	switch {
	case e.state == entryValid:
		return e.session
	case e.disabled != nil:
		return &authn.Session{Account: e.disabled}
	}
	return nil
}

// ForAccount returns accountID's live session, or nil. Unlike Select it never
// returns the disabled sentinel; tickets bound to an account use it.
func (b *Browser) ForAccount(accountID int32) *authn.Session {
	e := b.find(accountID)
	if e == nil {
		return nil
	}
	b.check(e)
	if e.state == entryValid {
		return e.session
	}
	return nil
}

// ForSessionID returns the live session whose SessionID is sid, or nil.
func (b *Browser) ForSessionID(sid string) *authn.Session {
	if sid == "" {
		return nil
	}
	for _, e := range b.ordered() {
		if e.session.Data != nil && e.session.Data.SessionID == sid {
			return e.session
		}
	}
	return nil
}

// Has reports whether accountID is signed in here.
func (b *Browser) Has(accountID int32) bool { return b.ForAccount(accountID) != nil }

// CanAttach reports whether accountID can be signed in: it already is (its
// entry is replaced in place), or the browser is below the cap.
func (b *Browser) CanAttach(accountID int32) bool {
	if b.Has(accountID) {
		return true
	}
	return b.held() < b.m.Limit()
}

// AtLimit reports whether no further account can be signed in.
func (b *Browser) AtLimit() bool { return b.held() >= b.m.Limit() }

// ErrAccountLimit is returned by Attach when the browser is at its cap.
var ErrAccountLimit = errors.New("session: browser account limit reached")

// AttachParams describes the session Attach issues.
type AttachParams struct {
	AccountID     int32
	AMR           []string
	UpstreamIDPID *int64
	Remember      bool
}

// Attach issues a session for p.AccountID and writes it into this browser's
// cookies (the header of w immediately, and again when the response is
// written if anything else changes):
//
//   - an account already signed in keeps its Seq; its old session is revoked
//     and its entry replaced, moving to the other cookie when the remember
//     choice changed. Re-authenticating moves neither the order nor the
//     default account.
//   - a new account is appended with Seq = now, unless the browser is at its
//     cap, in which case nothing is issued and ErrAccountLimit returned.
//
// Expired entries are dropped on the way.
func (b *Browser) Attach(w http.ResponseWriter, p AttachParams) (*authn.Session, error) {
	b.checkAll()
	existing := b.find(p.AccountID)
	if existing != nil && existing.state != entryValid {
		existing = nil
	}
	if existing == nil && b.held() >= b.m.Limit() {
		return nil, ErrAccountLimit
	}
	token, data, err := b.m.store.Issue(b.ctx, p.AccountID, b.m.ipOf(b.r), b.r.UserAgent(), p.AMR, p.UpstreamIDPID, p.Remember)
	if err != nil {
		return nil, err
	}
	account, err := b.m.account(b.ctx, p.AccountID)
	if err != nil {
		_ = b.m.store.Revoke(b.ctx, p.AccountID, token)
		return nil, err
	}
	sess := &authn.Session{Account: &account, Token: token, Data: data}
	if existing != nil {
		_ = b.m.store.Revoke(b.ctx, existing.AccountID, existing.Token)
		b.markDirty(existing.Remember)
		existing.Token = token
		existing.Remember = p.Remember
		existing.session = sess
		b.markDirty(p.Remember)
	} else {
		seq := time.Now().UnixMilli()
		for _, e := range b.entries {
			if e.Seq >= seq {
				seq = e.Seq + 1
			}
		}
		// A stale entry for the same account (its sessions revoked by a
		// reset, or expired) gives way to the new one but keeps its place.
		kept := b.entries[:0]
		for _, e := range b.entries {
			if e.AccountID != p.AccountID {
				kept = append(kept, e)
			} else if e.Seq > 0 && e.disabled == nil {
				seq = e.Seq
			}
		}
		b.entries = append(kept, &browserEntry{
			Entry:   Entry{Seq: seq, AccountID: p.AccountID, Token: token, Remember: p.Remember},
			state:   entryValid,
			session: sess,
		})
		b.markDirty(p.Remember)
	}
	if w != nil {
		b.Flush(w.Header())
	}
	return sess, nil
}

// Remove signs accountID out of this browser: its session is revoked and its
// entry dropped. It reports whether the account was signed in.
func (b *Browser) Remove(accountID int32) bool {
	e := b.find(accountID)
	if e == nil || e.state == entryGone {
		return false
	}
	b.drop(e, true)
	return true
}

// RemoveAll revokes every entry and clears both cookies. It returns the
// account IDs whose sessions were live.
func (b *Browser) RemoveAll() []int32 {
	var ids []int32
	for _, e := range b.entries {
		b.check(e)
		if e.state == entryValid || e.state == entryUnavailable {
			ids = append(ids, e.AccountID)
		}
		if e.state != entryGone {
			b.drop(e, true)
		}
	}
	b.dirtyPersistent, b.dirtyTemp = true, true
	return ids
}

// Flush writes the cookies that changed into h, replacing any earlier
// Set-Cookie for them.
func (b *Browser) Flush(h http.Header) {
	if !b.Dirty() {
		return
	}
	// An unchecked legacy entry has no Seq or cookie yet; settle it first.
	for _, e := range b.entries {
		if e.legacy {
			b.check(e)
		}
	}
	var entries []Entry
	for _, e := range b.entries {
		if e.state == entryGone || e.legacy {
			continue
		}
		entries = append(entries, e.Entry)
	}
	writeBrowserCookies(h, b.m.cfg, entries, b.m.store.rememberTTL, b.dirtyPersistent, b.dirtyTemp)
	b.dirtyPersistent, b.dirtyTemp = false, false
}
