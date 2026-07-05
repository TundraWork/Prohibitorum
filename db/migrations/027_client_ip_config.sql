-- +goose Up
-- Client-IP resolution policy: how the effective remote/user IP is extracted from
-- forwarding headers behind a CDN/reverse proxy. strategy is 'direct' (peer only),
-- 'forwarded' (X-Forwarded-For), or 'header' (a single named header, e.g.
-- CF-Connecting-IP). A header is trusted only when the direct peer is inside one of
-- client_ip_trusted_proxies (CIDRs); an empty set means headers are never trusted.
ALTER TABLE instance_settings
  ADD COLUMN IF NOT EXISTS client_ip_strategy        text   NOT NULL DEFAULT 'direct',
  ADD COLUMN IF NOT EXISTS client_ip_header          text   NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS client_ip_trusted_proxies text[] NOT NULL DEFAULT '{}';

-- +goose Down
ALTER TABLE instance_settings
  DROP COLUMN IF EXISTS client_ip_strategy,
  DROP COLUMN IF EXISTS client_ip_header,
  DROP COLUMN IF EXISTS client_ip_trusted_proxies;
