package vrchat

import "testing"

func TestParseIdentityCanonicalTable(t *testing.T) {
	valid := []string{testUserID, "https://vrchat.com/home/user/" + testUserID}
	for _, input := range valid {
		got, err := parseIdentity(input)
		if err != nil || got != testUserID {
			t.Errorf("parseIdentity(%q) = %q, %v", input, got, err)
		}
	}
	invalid := []string{
		"USR_01234567-89ab-cdef-0123-456789abcdef",
		"usr_01234567-89AB-cdef-0123-456789abcdef",
		"usr_not-a-uuid",
		"http://vrchat.com/home/user/" + testUserID,
		"https://vrchat.com.evil.example/home/user/" + testUserID,
		"https://vrchat.com:443/home/user/" + testUserID,
		"https://user@vrchat.com/home/user/" + testUserID,
		"https://vrchat.com/home/user/" + testUserID + "/",
		"https://vrchat.com/home/user/" + testUserID + "/extra",
		"https://vrchat.com/home%2fuser/" + testUserID,
		"https://vrchat.com/home%252fuser/" + testUserID,
		"https://vrchat.com/home/user/" + testUserID + "?x=1",
		"https://vrchat.com/home/user/" + testUserID + "#x",
	}
	for _, input := range invalid {
		if got, err := parseIdentity(input); err == nil {
			t.Errorf("parseIdentity(%q) unexpectedly accepted as %q", input, got)
		}
	}
}

func TestProofLinkCanonicalOriginAndToken(t *testing.T) {
	token := "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	origin := "https://login.example.com"
	valid := []string{
		origin + "/verify/vrchat/" + token,
		"https://LOGIN.EXAMPLE.COM/verify/vrchat/" + token,
		"https://login.example.com:443/verify/vrchat/" + token,
	}
	for _, link := range valid {
		if !proofLinkMatches(link, origin, token) {
			t.Errorf("proofLinkMatches(%q) rejected", link)
		}
	}
	invalid := []string{
		"http://login.example.com/verify/vrchat/" + token,
		"https://evil.example/verify/vrchat/" + token,
		"https://login.example.com:444/verify/vrchat/" + token,
		"https://user@login.example.com/verify/vrchat/" + token,
		origin + "/verify/vrchat/" + token + "?x=1",
		origin + "/verify/vrchat/" + token + "#x",
		origin + "/verify%2fvrchat/" + token,
		origin + "/verify%252fvrchat/" + token,
		origin + "/verify/vrchat/wrong",
	}
	for _, link := range invalid {
		if proofLinkMatches(link, origin, token) {
			t.Errorf("proofLinkMatches(%q) unexpectedly accepted", link)
		}
	}
}
