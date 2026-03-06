package app

import (
	"fmt"
	"log"
	"os"
)

// validateConfig checks for critical misconfigurations.
//
// Fatal conditions return a non-nil error that prevents startup.
// Non-fatal conditions emit [WARN] log lines; production deployments should
// have zero warnings before going live.
func (a *App) validateConfig() error {
	cfg := a.Config

	// 1. Cooperative shared secret — fatal when cooperative auth is enabled.
	//    All nodes in the same earning cooperative must share this secret.
	if cfg.Radius.Enabled {
		if cfg.Radius.SharedSecret == "" {
			return fmt.Errorf("FATAL: cooperative auth enabled (radius.enabled=true) but radius.shared_secret is empty — " +
				"set a strong cooperative secret via SOHOLINK_RADIUS_SHARED_SECRET or the config file")
		}
		if cfg.Radius.SharedSecret == "testing123" {
			return fmt.Errorf("FATAL: radius.shared_secret is the insecure default 'testing123' — " +
				"set a strong cooperative secret via SOHOLINK_RADIUS_SHARED_SECRET before enabling cooperative mode")
		}
	} else if cfg.Radius.SharedSecret == "testing123" {
		log.Printf("[WARN] SECURITY: radius.shared_secret is 'testing123'. Change it before enabling cooperative auth.")
	}

	// 2. Payment enabled but no real money-movement processor configured.
	if cfg.Payment.Enabled {
		hasReal := false
		for _, p := range cfg.Payment.Processors {
			if p.Type == "stripe" || p.Type == "lightning" {
				hasReal = true
				break
			}
		}
		if !hasReal {
			log.Printf("[WARN] payment.enabled=true but no Stripe or Lightning processor configured. Only barter (local credits) will work.")
		}
	}

	// 3. Stripe processor configured but secret key env var is unset.
	for _, p := range cfg.Payment.Processors {
		if p.Type == "stripe" && p.SecretKeyEnv != "" {
			if os.Getenv(p.SecretKeyEnv) == "" {
				log.Printf("[WARN] Stripe processor configured but $%s is not set. Stripe charges will fail.", p.SecretKeyEnv)
			}
		}
		// 4. Lightning processor has no TLS cert path.
		if p.Type == "lightning" && p.LNDTLSCertPath == "" && p.LNDHost != "" {
			log.Printf("[WARN] Lightning processor (lnd_host=%s) has no lnd_tls_cert_path — TLS verification is disabled. Configure cert pinning for production.", p.LNDHost)
		}
	}

	// 5. Stripe payment enabled but no webhook secret — async events not processed.
	if cfg.Payment.Enabled && cfg.Payment.StripeWebhookSecret == "" {
		for _, p := range cfg.Payment.Processors {
			if p.Type == "stripe" {
				log.Printf("[WARN] Stripe processor configured but payment.stripe_webhook_secret is empty. " +
					"Set SOHOLINK_PAYMENT_STRIPE_WEBHOOK_SECRET to enable webhook-driven payment confirmation.")
				break
			}
		}
	}

	// 6. Federation coordinator declared but resource-sharing HTTP API is off.
	if cfg.Federation.IsCoordinator && !cfg.ResourceSharing.Enabled {
		log.Printf("[WARN] federation.is_coordinator=true but resource_sharing.enabled=false. The coordinator HTTP API will not be reachable.")
	}

	// 7. Node DID is empty — federation announcements will be rejected.
	if cfg.Node.DID == "" {
		log.Printf("[WARN] node.did is empty. Federation announcements and blockchain anchoring will not work correctly.")
	}

	// 8. Orchestration without billing — workloads run but nothing is charged.
	if cfg.Orchestration.Enabled && !cfg.Payment.Enabled {
		log.Printf("[INFO] orchestration.enabled=true but payment.enabled=false. Workloads will run but no billing will occur.")
	}

	// 9. TLS not configured — plain HTTP in production is unsafe.
	if cfg.ResourceSharing.Enabled &&
		(cfg.ResourceSharing.TLSCertFile == "" || cfg.ResourceSharing.TLSKeyFile == "") {
		log.Printf("[WARN] TLS is not configured (resource_sharing.tls_cert_file/tls_key_file are empty). " +
			"The API runs over plain HTTP — set these fields for HTTPS in production.")
	}

	return nil
}
