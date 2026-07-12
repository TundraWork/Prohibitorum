-- +goose Up
-- allow_private_network is a per-IdP policy (default false) that controls
-- whether the outbound federation client's dial-time internal-IP screen is
-- disabled for THIS IdP only. When true, only RFC1918 IPv4 and IPv6 ULA
-- destinations become eligible for this IdP's discovery, JWKS, token,
-- userinfo, and inherited-avatar requests. Loopback, link-local/cloud
-- metadata, multicast, unspecified, reserved, and non-routable
-- special-use destinations remain blocked unconditionally.
-- This replaces the former global federation.allow_private_network config
-- bypass (audit follow-up N2).
ALTER TABLE upstream_idp
  ADD COLUMN IF NOT EXISTS allow_private_network boolean NOT NULL DEFAULT false;

-- +goose Down
ALTER TABLE upstream_idp DROP COLUMN IF EXISTS allow_private_network;
