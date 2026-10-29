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
	// Delete was released without parameters.
	deleteReport := func(r *RegulatoryReportsService, opts ...option.RequestOption) error {
		return r.Delete(context.Background(), "report-uuid-1", opts...)
	}
	_ = deleteReport

	// The released types of the Travel Rule settings networks.
	released := func(
		TravelRuleSettingsDetailNetworksGtr, TravelRuleSettingsDetailNetworksTrust,
		TravelRuleSettingsDetailNetworksVerifyvasp, TravelRuleSettingsDetailNetworksSygna,
		TravelRuleSettingsDetailNetworksGtrRail, TravelRuleSettingsDetailNetworksTrustRail,
		TravelRuleSettingsDetailNetworksVerifyvaspRail, TravelRuleSettingsDetailNetworksSygnaRail,
		TravelRuleSettingsDetailNetworksGtrMode, TravelRuleSettingsDetailNetworksTrustMode,
		TravelRuleSettingsDetailNetworksVerifyvaspMode, TravelRuleSettingsDetailNetworksSygnaMode,
	) {
	}
	var n TravelRuleSettingsDetailNetworks
	released(n.Gtr, n.Trust, n.Verifyvasp, n.Sygna,
		n.Gtr.Rail, n.Trust.Rail, n.Verifyvasp.Rail, n.Sygna.Rail,
		n.Gtr.Mode, n.Trust.Mode, n.Verifyvasp.Mode, n.Sygna.Mode)

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
