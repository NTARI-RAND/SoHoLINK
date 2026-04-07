package payment

import (
	"context"
	"fmt"

	stripe "github.com/stripe/stripe-go/v82"
)

// OnboardingStatus reports whether a connected account is ready to receive payouts.
type OnboardingStatus struct {
	// TransfersActive is true when the transfers capability is active.
	TransfersActive bool
	// RequirementsPending is true when currently_due or past_due fields exist.
	RequirementsPending bool
}

// CreateConnectedAccount creates an Express connected account for the given
// provider. The account is configured for manual payouts so NTARI controls
// the 24-hour payout hold.
func (c *Client) CreateConnectedAccount(ctx context.Context, displayName, email string) (string, error) {
	params := &stripe.AccountCreateParams{
		Type:  stripe.String("express"),
		Email: stripe.String(email),
		BusinessProfile: &stripe.AccountCreateBusinessProfileParams{
			Name: stripe.String(displayName),
		},
		Capabilities: &stripe.AccountCreateCapabilitiesParams{
			Transfers: &stripe.AccountCreateCapabilitiesTransfersParams{
				Requested: stripe.Bool(true),
			},
		},
		Controller: &stripe.AccountCreateControllerParams{
			Fees: &stripe.AccountCreateControllerFeesParams{
				Payer: stripe.String("application"),
			},
			Losses: &stripe.AccountCreateControllerLossesParams{
				Payments: stripe.String("application"),
			},
			StripeDashboard: &stripe.AccountCreateControllerStripeDashboardParams{
				Type: stripe.String("express"),
			},
		},
		Settings: &stripe.AccountCreateSettingsParams{
			Payouts: &stripe.AccountCreateSettingsPayoutsParams{
				Schedule: &stripe.AccountCreateSettingsPayoutsScheduleParams{
					Interval: stripe.String("manual"),
				},
			},
		},
	}

	acct, err := c.sc.V1Accounts.Create(ctx, params)
	if err != nil {
		return "", fmt.Errorf("create connected account: %w", err)
	}
	return acct.ID, nil
}

// CreateOnboardingLink generates a Stripe-hosted onboarding URL for the given
// connected account. The provider must complete onboarding before receiving
// payouts. refreshURL is called if the link expires; returnURL is the
// destination after completion.
func (c *Client) CreateOnboardingLink(ctx context.Context, accountID, refreshURL, returnURL string) (string, error) {
	params := &stripe.AccountLinkCreateParams{
		Account:    stripe.String(accountID),
		RefreshURL: stripe.String(refreshURL),
		ReturnURL:  stripe.String(returnURL),
		Type:       stripe.String("account_onboarding"),
	}

	link, err := c.sc.V1AccountLinks.Create(ctx, params)
	if err != nil {
		return "", fmt.Errorf("create onboarding link: %w", err)
	}
	return link.URL, nil
}

// CheckOnboardingStatus retrieves the connected account and reports whether it
// is ready to receive payouts. A provider is ready when transfers capability is
// active and there are no currently_due or past_due requirements.
func (c *Client) CheckOnboardingStatus(ctx context.Context, accountID string) (OnboardingStatus, error) {
	params := &stripe.AccountRetrieveParams{}
	params.AddExpand("capabilities")

	acct, err := c.sc.V1Accounts.GetByID(ctx, accountID, params)
	if err != nil {
		return OnboardingStatus{}, fmt.Errorf("retrieve account: %w", err)
	}

	status := OnboardingStatus{
		TransfersActive: acct.Capabilities != nil &&
			acct.Capabilities.Transfers == stripe.AccountCapabilityStatusActive,
	}

	if acct.Requirements != nil {
		status.RequirementsPending = len(acct.Requirements.CurrentlyDue) > 0 ||
			len(acct.Requirements.PastDue) > 0
	}

	return status, nil
}
