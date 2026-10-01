package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

type acquireCauseTransport struct{ err error }
type acquireCauseDecryptFailure struct{}

func (acquireCauseDecryptFailure) Encrypt(string) (string, error) {
	return "", errors.New("synthetic encryption failure")
}
func (acquireCauseDecryptFailure) Decrypt(string) (string, error) {
	return "", errors.New("synthetic decryption failure")
}

func (f acquireCauseTransport) Bootstrap(context.Context, *Account, map[string]any, any) (ConsumerWebBootstrapResult, error) {
	return ConsumerWebBootstrapResult{}, f.err
}
func (f acquireCauseTransport) RotatePSIDTS(context.Context, *Account, ConsumerWebSessionHandle) (ConsumerWebRotationResult, error) {
	return ConsumerWebRotationResult{}, nil
}
func (f acquireCauseTransport) Close(context.Context, ConsumerWebSessionHandle) error { return nil }

func TestConsumerWebAcquireCauseSynthetic(t *testing.T) {
	secret := "synthetic-secret-must-not-appear"
	for _, tc := range []struct {
		name  string
		cause ConsumerWebAcquireCause
		err   error
	}{
		{"credential_missing", ConsumerWebAcquireCredentialNamespaceMissing, ErrConsumerWebAuthLost},
		{"decrypt_fail", ConsumerWebAcquireCredentialDecryptFailed, fmt.Errorf("%w: %s", ErrConsumerWebAuthLost, secret)},
		{"app_token_missing", ConsumerWebAcquireAPPTokenMissing, ErrConsumerWebAPPTokenMissing},
		{"transport_fail", ConsumerWebAcquireAPPTransportFailure, fmt.Errorf("%w: %s", ErrConsumerWebTransportTransient, secret)},
		{"unresolved_auth", ConsumerWebAcquireUnknown, ErrConsumerWebAuthUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			original := &ConsumerWebAcquireError{Cause: tc.cause, Err: tc.err}
			manager := NewConsumerWebSessionManager(ConsumerWebSessionManagerOptions{
				Transport:   acquireCauseTransport{err: original},
				Credentials: &sessionFakeCredentials{values: map[string]any{}},
			})
			defer manager.Close()
			_, err := manager.AcquireSession(context.Background(), testSessionAccount(42))
			var got *ConsumerWebAcquireError
			if !errors.As(err, &got) || got.Cause != tc.cause || !errors.Is(err, tc.err) {
				t.Fatalf("typed cause or unwrap mismatch: %v", err)
			}
			if strings.Contains(err.Error(), secret) {
				t.Fatal("secret leaked in diagnostic error")
			}
			// A task keeps the typed chain internally while JSON status remains coarse.
			snapshot := ConsumerWebVideoTaskSnapshot{Error: "AUTH_REQUIRED", AcquireSessionCause: got.Cause, AcquireSessionError: err}
			var throughTask *ConsumerWebAcquireError
			if !errors.As(snapshot.AcquireSessionError, &throughTask) || throughTask.Cause != tc.cause {
				t.Fatal("task did not preserve typed chain")
			}
			encoded, _ := json.Marshal(snapshot)
			if strings.Contains(string(encoded), string(tc.cause)) || strings.Contains(string(encoded), secret) {
				t.Fatal("internal diagnostic leaked to public task status")
			}
		})
	}
}

func TestConsumerWebAcquireCauseProductionCredentialBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name        string
		credentials map[string]any
		want        ConsumerWebAcquireCause
	}{
		{"missing_namespace", map[string]any{}, ConsumerWebAcquireCredentialNamespaceMissing},
		{"decrypt_failure", map[string]any{ConsumerWebCredentialNamespace: map[string]any{
			ConsumerWebPSIDCiphertextKey:   "invalid-ciphertext",
			ConsumerWebPSIDTSCiphertextKey: "invalid-ciphertext",
		}}, ConsumerWebAcquireCredentialDecryptFailed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			transport := NewConsumerWebHTTPTransport(ConsumerWebHTTPTransportOptions{
				HTTPUpstream: consumerWebTLSFailureUpstream{err: ErrConsumerWebTransportTransient},
				Encryptor:    acquireCauseDecryptFailure{},
			})
			manager := NewConsumerWebSessionManager(ConsumerWebSessionManagerOptions{
				Transport:   transport,
				Credentials: &sessionFakeCredentials{values: tc.credentials},
			})
			defer manager.Close()
			_, err := manager.AcquireSession(context.Background(), testSessionAccount(43))
			var typed *ConsumerWebAcquireError
			if !errors.As(err, &typed) || typed.Cause != tc.want {
				t.Fatalf("unexpected safe cause: %v", err)
			}
		})
	}
}

type acquireCauseVideoTransport struct{ videoTaskFixtureTransport }

func (f *acquireCauseVideoTransport) Bootstrap(context.Context, *Account, map[string]any, any) (ConsumerWebBootstrapResult, error) {
	return ConsumerWebBootstrapResult{}, &ConsumerWebAcquireError{Cause: ConsumerWebAcquireCredentialDecryptFailed, Err: ErrConsumerWebAuthLost}
}

func TestConsumerWebVideoTaskPreservesAcquireCauseInternally(t *testing.T) {
	transport := &acquireCauseVideoTransport{}
	manager := NewConsumerWebSessionManager(ConsumerWebSessionManagerOptions{
		Transport:   transport,
		Credentials: &sessionFakeCredentials{values: map[string]any{}},
	})
	defer manager.Close()
	tasks := NewConsumerWebVideoTaskService(manager, transport)
	created, err := tasks.Start(3, testSessionAccount(45), "gemini-3.1-pro-high", "synthetic", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		status, err := tasks.Status(context.Background(), created.ID, 3)
		if err != nil {
			t.Fatal(err)
		}
		if status.Status == ConsumerWebVideoFailed {
			var typed *ConsumerWebAcquireError
			if status.Error != "AUTH_REQUIRED" || status.AcquireSessionCause != ConsumerWebAcquireCredentialDecryptFailed || !errors.As(status.AcquireSessionError, &typed) {
				t.Fatal("task lost safe acquire cause or typed error chain")
			}
			encoded, _ := json.Marshal(status)
			if strings.Contains(string(encoded), string(status.AcquireSessionCause)) {
				t.Fatal("internal cause leaked publicly")
			}
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("task did not finish")
}
