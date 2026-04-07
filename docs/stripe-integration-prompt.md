# Stripe Connect Integration Spec — SoHoLINK

## Platform Model
- **Type:** Buyers purchase from NTARI (platform); NTARI pays out to sellers
- **Payout model:** Split payouts — single consumer charge split across multiple providers for partitioned workloads
- **Industry:** On-demand services
- **Seller account management:** Express Dashboard (Stripe-hosted)
- **Seller onboarding:** Stripe-hosted onboarding

## Connected Account Creation
> **Note:** `V2CoreAccountCreateParams` and related V2 account types do not exist in
> `stripe-go/v82` (v82.5.1). The V2 namespace in this SDK version covers only billing
> meter events and core event destinations. Use the V1 account API instead.

Create an Express account with application-collected fees/losses and manual payouts:
```go
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
acct, err := client.V1Accounts.Create(ctx, params)
```

The `Settings.Payouts.Schedule.Interval = "manual"` replaces the separate post-creation
update step — manual payouts are configured at account creation time.

## Onboarding Links
```go
params := &stripe.AccountLinkCreateParams{
    Account:    stripe.String(accountID),
    RefreshURL: stripe.String(refreshURL),
    ReturnURL:  stripe.String(returnURL),
    Type:       stripe.String("account_onboarding"),
}
link, err := client.V1AccountLinks.Create(ctx, params)
// link.URL is the Stripe-hosted onboarding URL
```

## Onboarding Status Check
```go
params := &stripe.AccountRetrieveParams{}
params.AddExpand("capabilities")
acct, err := client.V1Accounts.GetByID(ctx, accountID, params)

transfersActive := acct.Capabilities != nil &&
    acct.Capabilities.Transfers == stripe.AccountCapabilityStatusActive
requirementsPending := len(acct.Requirements.CurrentlyDue) > 0 ||
    len(acct.Requirements.PastDue) > 0
readyToReceivePayments := transfersActive && !requirementsPending
```

## Charges — Destination Charge with Application Fee
```go
params := &stripe.PaymentIntentCreateParams{
    Amount:   stripe.Int64(amountCents),
    Currency: stripe.String("usd"),
    PaymentIntentCreateParams: stripe.PaymentIntentCreateParams{
        ApplicationFeeAmount: stripe.Int64(feeCents),
        TransferData: &stripe.PaymentIntentTransferDataParams{
            Destination: stripe.String(connectedAccountID),
        },
    },
}
```

All amounts in cents (int64). Platform fee deducted automatically at settlement.

## Payout Hold — 24-Hour Dispute Window
Connected accounts are configured with manual payouts. After settlement,
funds sit in the connected account's Stripe balance for 24 hours before
NTARI triggers the payout. This is implemented by setting
`settings.payouts.schedule.interval = "manual"` on the connected account
and calling `stripe.Payout.Create` after the hold window expires.

## Dispute Escrow
When a dispute is filed, the NTARI Dispute Terminal calls
`stripe.PaymentIntent.Cancel` or issues a partial/full refund via
`stripe.Refund.Create`. The arbiter controls the split via the terminal UI.

## Webhook Events (Thin Events — V2)
Listen for:
- `v2.core.account[requirements].updated`
- `v2.core.account[.recipient].capability_status_updated`

Parse thin events:
```go
thinEvent := client.ParseThinEvent(body, sig, webhookSecret)
event, err := client.V2CoreEvents.Retrieve(thinEvent.ID, nil)
// use event.Type to route handling
```

## Environment Variables
- `STRIPE_SECRET_KEY` — Stripe secret key (never commit)
- `STRIPE_PUBLISHABLE_KEY` — Stripe publishable key
- `STRIPE_WEBHOOK_SECRET` — webhook signing secret

## SDK Version
Use `github.com/stripe/stripe-go/v82` — already in go.mod.
Do not set the API version manually; the SDK sets it automatically.
