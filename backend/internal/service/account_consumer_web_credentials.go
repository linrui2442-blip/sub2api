package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

const (
	ConsumerWebCredentialNamespace = "consumer_web"
	ConsumerWebPSIDCiphertextKey   = "__Secure-1PSID_ciphertext"
	ConsumerWebPSIDTSCiphertextKey = "__Secure-1PSIDTS_ciphertext"
)

var ErrInvalidConsumerWebCredential = errors.New("invalid consumer web credential")

type ConditionalConsumerWebPSIDTSPersister interface {
	PersistPSIDTS(ctx context.Context, accountID int64, expectedRevision any, psidts string) error
}

type conditionalConsumerWebPSIDTSPersister struct {
	repo      AccountRepository
	encryptor SecretEncryptor
}

func NewConditionalConsumerWebPSIDTSPersister(repo AccountRepository, encryptor SecretEncryptor) ConditionalConsumerWebPSIDTSPersister {
	return &conditionalConsumerWebPSIDTSPersister{repo: repo, encryptor: encryptor}
}

func (p *conditionalConsumerWebPSIDTSPersister) PersistPSIDTS(ctx context.Context, accountID int64, expectedRevision any, psidts string) error {
	patch, err := EncryptConsumerWebCredentialPatch(p.encryptor, nil, &psidts)
	if err != nil {
		return err
	}
	_, err = MutateAccountCredentialsConditional(ctx, p.repo, accountID, expectedRevision, func(current map[string]any) (map[string]any, error) {
		return UpdateAccountCredentialNamespace(current, ConsumerWebCredentialNamespace, patch), nil
	})
	return err
}

type ConsumerWebCredentialStatus struct {
	ConsumerWebCredentialsPresent bool `json:"consumer_web_credentials_present"`
	HasSecure1PSID                bool `json:"has_secure_1psid"`
	HasSecure1PSIDTS              bool `json:"has_secure_1psidts"`
}

func cloneCredentialMap(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for key, value := range in {
		if nested, ok := value.(map[string]any); ok {
			out[key] = cloneCredentialMap(nested)
		} else {
			out[key] = value
		}
	}
	return out
}

// UpdateAccountCredentialNamespace applies an explicit non-nil patch without
// mutating either input. Omitted and nil values preserve stored credentials.
func UpdateAccountCredentialNamespace(existing map[string]any, namespace string, patch map[string]any) map[string]any {
	out := cloneCredentialMap(existing)
	nested, _ := out[namespace].(map[string]any)
	nested = cloneCredentialMap(nested)
	for key, value := range patch {
		if value != nil {
			nested[key] = value
		}
	}
	if len(nested) > 0 {
		out[namespace] = nested
	}
	return out
}

func EncryptConsumerWebCredentialPatch(encryptor SecretEncryptor, psid, psidts *string) (map[string]any, error) {
	if encryptor == nil {
		return nil, errors.New("consumer web credential encryption is unavailable")
	}
	normalizedPSID, err := normalizeConsumerWebCookieValue("__Secure-1PSID", psid)
	if err != nil {
		return nil, err
	}
	normalizedPSIDTS, err := normalizeConsumerWebCookieValue("__Secure-1PSIDTS", psidts)
	if err != nil {
		return nil, err
	}
	patch := make(map[string]any, 2)
	for key, value := range map[string]*string{
		ConsumerWebPSIDCiphertextKey:   normalizedPSID,
		ConsumerWebPSIDTSCiphertextKey: normalizedPSIDTS,
	} {
		if value == nil {
			continue
		}
		ciphertext, err := encryptor.Encrypt(*value)
		if err != nil {
			return nil, errors.New("consumer web credential encryption failed")
		}
		patch[key] = ciphertext
	}
	return patch, nil
}

func normalizeConsumerWebCookieValue(field string, supplied *string) (*string, error) {
	if supplied == nil {
		return nil, nil
	}

	value := strings.TrimSpace(*supplied)
	value = strings.Trim(value, "\"")
	value = strings.Trim(value, "'")
	value = strings.TrimSuffix(value, ";")

	invalid := value == "" ||
		strings.ContainsAny(value, "\r\n") ||
		strings.HasPrefix(value, "__Secure-1PSID=") ||
		strings.HasPrefix(value, "__Secure-1PSIDTS=") ||
		strings.HasPrefix(strings.ToLower(value), "cookie:") ||
		strings.Contains(value, ";")
	if invalid {
		return nil, fmt.Errorf("%w: invalid %s format", ErrInvalidConsumerWebCredential, field)
	}
	return &value, nil
}

func DecryptConsumerWebCredentials(encryptor SecretEncryptor, credentials map[string]any) (psid, psidts string, err error) {
	if encryptor == nil {
		return "", "", errors.New("consumer web credential encryption is unavailable")
	}
	nested, _ := credentials[ConsumerWebCredentialNamespace].(map[string]any)
	decrypt := func(key string) (string, error) {
		ciphertext, _ := nested[key].(string)
		if strings.TrimSpace(ciphertext) == "" {
			return "", nil
		}
		return encryptor.Decrypt(ciphertext)
	}
	if psid, err = decrypt(ConsumerWebPSIDCiphertextKey); err != nil {
		return "", "", err
	}
	if psidts, err = decrypt(ConsumerWebPSIDTSCiphertextKey); err != nil {
		return "", "", err
	}
	return psid, psidts, nil
}

func ConsumerWebCredentialsStatus(credentials map[string]any) ConsumerWebCredentialStatus {
	nested, _ := credentials[ConsumerWebCredentialNamespace].(map[string]any)
	present := func(key string) bool {
		value, _ := nested[key].(string)
		return strings.TrimSpace(value) != ""
	}
	status := ConsumerWebCredentialStatus{
		HasSecure1PSID:   present(ConsumerWebPSIDCiphertextKey),
		HasSecure1PSIDTS: present(ConsumerWebPSIDTSCiphertextKey),
	}
	status.ConsumerWebCredentialsPresent = status.HasSecure1PSID && status.HasSecure1PSIDTS
	return status
}

func UpdateConsumerWebPSIDTS(existing map[string]any, encryptor SecretEncryptor, psidts string) (map[string]any, error) {
	patch, err := EncryptConsumerWebCredentialPatch(encryptor, nil, &psidts)
	if err != nil {
		return nil, err
	}
	return UpdateAccountCredentialNamespace(existing, ConsumerWebCredentialNamespace, patch), nil
}
