package federation_test

import (
	federationcore "prohibitorum/pkg/federation"
	federationoidc "prohibitorum/pkg/federation/providers/oidc"
	federationsteam "prohibitorum/pkg/federation/providers/steam"
)

var _ federationcore.Definition = federationoidc.Definition{}
var _ federationcore.Adapter = (*federationoidc.Adapter)(nil)
var _ federationcore.Definition = federationsteam.Definition{}
var _ federationcore.Adapter = (*federationsteam.Adapter)(nil)
