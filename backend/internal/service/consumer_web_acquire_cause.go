package service

import "errors"

// ConsumerWebAcquireCause is a bounded, secret-free internal diagnostic.
type ConsumerWebAcquireCause string

const (
	ConsumerWebAcquireCredentialNamespaceMissing ConsumerWebAcquireCause = "CREDENTIAL_NAMESPACE_MISSING"
	ConsumerWebAcquireCredentialDecryptFailed    ConsumerWebAcquireCause = "CREDENTIAL_DECRYPT_FAILED"
	ConsumerWebAcquireAccountNotReady            ConsumerWebAcquireCause = "ACCOUNT_NOT_CONSUMER_WEB_READY"
	ConsumerWebAcquireSessionInitFailed          ConsumerWebAcquireCause = "SESSION_INIT_FAILED"
	ConsumerWebAcquireAPPTokenMissing            ConsumerWebAcquireCause = "APP_TOKEN_MISSING"
	ConsumerWebAcquireAPPTransportFailure        ConsumerWebAcquireCause = "APP_TRANSPORT_FAILURE"
	ConsumerWebAcquireProxyFailure               ConsumerWebAcquireCause = "PROXY_FAILURE"
	ConsumerWebAcquireUnknown                    ConsumerWebAcquireCause = "UNKNOWN"
)

type ConsumerWebAcquireError struct {
	Cause ConsumerWebAcquireCause
	Err   error
}

// Error intentionally never includes the wrapped error, which may contain secrets.
func (e *ConsumerWebAcquireError) Error() string { return string(e.Cause) }
func (e *ConsumerWebAcquireError) Unwrap() error { return e.Err }

func consumerWebAcquireCause(err error) ConsumerWebAcquireCause {
	var typed *ConsumerWebAcquireError
	if errors.As(err, &typed) {
		return typed.Cause
	}
	var transport *HTTPTransportDiagnosticError
	if errors.As(err, &transport) && transport.diag.ProxyConfigured && transport.diag.Stage == DiagnosticStageProxyNegotiation {
		return ConsumerWebAcquireProxyFailure
	}
	switch {
	case errors.Is(err, ErrConsumerWebAPPTokenMissing):
		return ConsumerWebAcquireAPPTokenMissing
	case errors.Is(err, ErrConsumerWebTransportTransient), errors.Is(err, ErrConsumerWebRedirectRejected):
		return ConsumerWebAcquireAPPTransportFailure
	case errors.Is(err, ErrConsumerWebManagerClosed):
		return ConsumerWebAcquireSessionInitFailed
	default:
		return ConsumerWebAcquireUnknown
	}
}

func wrapConsumerWebAcquireError(err error) error {
	if err == nil {
		return nil
	}
	var typed *ConsumerWebAcquireError
	if errors.As(err, &typed) {
		return err
	}
	return &ConsumerWebAcquireError{Cause: consumerWebAcquireCause(err), Err: err}
}
