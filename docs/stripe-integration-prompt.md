# Stripe Connect Integration Spec — SoHoLINK

## Platform Model
- **Type:** Buyers purchase from NTARI (platform); NTARI pays out to sellers
- **Payout model:** Split payouts — single consumer charge split across multiple providers for partitioned workloads
- **Industry:** On-demand services
- **Seller account management:** Express Dashboard (Stripe-hosted)
- **Seller onboarding:** Stripe-hosted onboarding

## Connected Account Creation (V2 API)
Use the V2 API. Never pass `type` at the top level.
```go
params := &stripe.V2CoreAccountCreateParams{
    DisplayName:  stripe.String(displayName),
    ContactEmail: stripe.String(email),
    Identity: &stripe.V2CoreAccountCreateIdentityParams{
        Country: stripe.String("us"),
    },
    Dashboard: stripe.String("express"),
    Defaults: &stripe.V2CoreAccountCreateDefaultsParams{
        Responsibilities: &stripe.V2CoreAccountCreateDefaultsResponsibilitiesParams{
            FeesCollector:   stripe.String("application"),
            LossesCollector: stripe.String("application"),
        },
    },
    Configuration: &stripe.V2CoreAccountCreateConfigurationParams{
        Recipient: &stripe.V2CoreAccountCreateConfigurationRecipientParams{
            Capabilities: &stripe.V2CoreAccountCreateConfigurationRecipientCapabilitiesParams{
                StripeBalance: &stripe.V2CoreAccountCreateConfigurationRecipientCapabilitiesStripeBalanceParams{
                    StripeTransfers: &stripe.V2CoreAccountCreateConfigurationRecipientCapabilitiesStripeBalanceStripeTransfersParams{
                        Requested: stripe.Bool(true),
                    },
                },
            },
        },
    },
}
```

## Onboarding Links (V2 API)
```go
params := &stripe.V2CoreAccountLinkCreateParams{
    Account: stripe.String(accountID),
    UseCase: &stripe.V2CoreAccountLinkCreateUseCaseParams{
        Type: stripe.String("account_onboarding"),
        AccountOnboarding: &stripe.V2CoreAccountLinkCreateUseCaseAccountOnboardingParams{
            Configurations: []*string{stripe.String("recipient")},
            RefreshURL:     stripe.String(refreshURL),
            ReturnURL:      stripe.String(returnURL),
        },
    },
}
```

## Onboarding Status Check (V2 API)
```go
account, err := client.V2CoreAccounts.Retrieve(accountID, &stripe.V2CoreAccountRetrieveParams{
    Include: []*string{
        stripe.String("configuration.recipient"),
        stripe.String("requirements"),
    },
})

readyToReceivePayments := account.Configuration.Recipient.Capabilities.StripeBalance.StripeTransfers.Status == "active"
requirementsStatus := account.Requirements.Summary.MinimumDeadline.Status
onboardingComplete := requirementsStatus != "currently_due" && requirementsStatus != "past_due"
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
