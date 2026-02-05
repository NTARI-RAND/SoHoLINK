package radius

import (
	"context"
	"fmt"
	"log"
	"net"
	"sync"

	"layeh.com/radius"

	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/accounting"
	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/policy"
	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/verifier"
)

// Server manages RADIUS authentication and accounting listeners.
type Server struct {
	authAddr     string
	acctAddr     string
	sharedSecret string

	verifier   *verifier.Verifier
	policyEng  *policy.Engine
	accounting *accounting.Collector

	authServer *radius.PacketServer
	acctServer *radius.PacketServer

	wg sync.WaitGroup
}

// NewServer creates a new RADIUS server.
func NewServer(authAddr, acctAddr, sharedSecret string,
	v *verifier.Verifier, p *policy.Engine, a *accounting.Collector) *Server {

	return &Server{
		authAddr:     authAddr,
		acctAddr:     acctAddr,
		sharedSecret: sharedSecret,
		verifier:     v,
		policyEng:    p,
		accounting:   a,
	}
}

// staticSecretSource implements radius.SecretSource with a static shared secret.
type staticSecretSource struct {
	secret []byte
}

func (s *staticSecretSource) RADIUSSecret(ctx context.Context, remoteAddr net.Addr) ([]byte, error) {
	return s.secret, nil
}

// Start begins listening for RADIUS packets on auth and accounting ports.
func (s *Server) Start() error {
	secretSource := &staticSecretSource{secret: []byte(s.sharedSecret)}

	handler := &Handler{
		verifier:   s.verifier,
		policyEng:  s.policyEng,
		accounting: s.accounting,
	}

	// Auth server
	s.authServer = &radius.PacketServer{
		Addr:         s.authAddr,
		SecretSource: secretSource,
		Handler:      radius.HandlerFunc(handler.HandleAuth),
	}

	// Accounting server
	s.acctServer = &radius.PacketServer{
		Addr:         s.acctAddr,
		SecretSource: secretSource,
		Handler:      radius.HandlerFunc(handler.HandleAccounting),
	}

	// Start auth listener
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		log.Printf("[radius] auth server listening on %s", s.authAddr)
		if err := s.authServer.ListenAndServe(); err != nil {
			log.Printf("[radius] auth server stopped: %v", err)
		}
	}()

	// Start accounting listener
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		log.Printf("[radius] accounting server listening on %s", s.acctAddr)
		if err := s.acctServer.ListenAndServe(); err != nil {
			log.Printf("[radius] accounting server stopped: %v", err)
		}
	}()

	return nil
}

// Shutdown gracefully stops both RADIUS servers.
func (s *Server) Shutdown(ctx context.Context) error {
	var errs []error

	if s.authServer != nil {
		if err := s.authServer.Shutdown(ctx); err != nil {
			errs = append(errs, fmt.Errorf("auth server shutdown: %w", err))
		}
	}

	if s.acctServer != nil {
		if err := s.acctServer.Shutdown(ctx); err != nil {
			errs = append(errs, fmt.Errorf("acct server shutdown: %w", err))
		}
	}

	s.wg.Wait()

	if len(errs) > 0 {
		return fmt.Errorf("shutdown errors: %v", errs)
	}
	return nil
}

// AuthAddr returns the authentication listen address.
func (s *Server) AuthAddr() string {
	return s.authAddr
}

// AcctAddr returns the accounting listen address.
func (s *Server) AcctAddr() string {
	return s.acctAddr
}
