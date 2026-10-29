package didit

import (
	"context"
	"testing"

	"github.com/didit-protocol/sdk-go/option"
)

// TestReleasedNamesCompile uses names of release 1.0 whose schema the API has
// since changed (openapi/compatibility.json). Code written against a 1.x
// release keeps compiling until the next major version (docs/DESIGN.md
// section 13), so dropping one of these names fails here.
func TestReleasedNamesCompile(t *testing.T) {
	var _ func(*RegulatoryReportsService, context.Context, string, ...option.RequestOption) error = (*RegulatoryReportsService).Delete

	var n TravelRuleSettingsDetailNetworks
	var (
		_ TravelRuleSettingsDetailNetworksGtr        = n.Gtr
		_ TravelRuleSettingsDetailNetworksTrust      = n.Trust
		_ TravelRuleSettingsDetailNetworksVerifyvasp = n.Verifyvasp
		_ TravelRuleSettingsDetailNetworksSygna      = n.Sygna
	)
	rails := map[TravelRuleNetworkStateRail]TravelRuleNetworkStateRail{
		TravelRuleSettingsDetailNetworksGtrRailGtr:               TravelRuleNetworkStateRailGtr,
		TravelRuleSettingsDetailNetworksTrustRailTrust:           TravelRuleNetworkStateRailTrust,
		TravelRuleSettingsDetailNetworksVerifyvaspRailVerifyvasp: TravelRuleNetworkStateRailVerifyvasp,
		TravelRuleSettingsDetailNetworksSygnaRailSygna:           TravelRuleNetworkStateRailSygna,
	}
	modes := map[TravelRuleNetworkStateMode][]TravelRuleNetworkStateMode{
		TravelRuleNetworkStateModeOwn:         {TravelRuleSettingsDetailNetworksGtrModeOwn, TravelRuleSettingsDetailNetworksTrustModeOwn, TravelRuleSettingsDetailNetworksVerifyvaspModeOwn, TravelRuleSettingsDetailNetworksSygnaModeOwn},
		TravelRuleNetworkStateModeDidit:       {TravelRuleSettingsDetailNetworksGtrModeDidit, TravelRuleSettingsDetailNetworksTrustModeDidit, TravelRuleSettingsDetailNetworksVerifyvaspModeDidit, TravelRuleSettingsDetailNetworksSygnaModeDidit},
		TravelRuleNetworkStateModeOff:         {TravelRuleSettingsDetailNetworksGtrModeOff, TravelRuleSettingsDetailNetworksTrustModeOff, TravelRuleSettingsDetailNetworksVerifyvaspModeOff, TravelRuleSettingsDetailNetworksSygnaModeOff},
		TravelRuleNetworkStateModeUnavailable: {TravelRuleSettingsDetailNetworksGtrModeUnavailable, TravelRuleSettingsDetailNetworksTrustModeUnavailable, TravelRuleSettingsDetailNetworksVerifyvaspModeUnavailable, TravelRuleSettingsDetailNetworksSygnaModeUnavailable},
	}
	var _ TravelRuleSettingsDetailNetworksGtrRail = TravelRuleNetworkStateRailGtr
	var _ TravelRuleSettingsDetailNetworksTrustMode = TravelRuleNetworkStateModeOwn
	for old, now := range rails {
		if old != now {
			t.Errorf("released rail %q != %q", old, now)
		}
	}
	for now, olds := range modes {
		for _, old := range olds {
			if old != now {
				t.Errorf("released mode %q != %q", old, now)
			}
		}
	}
}
