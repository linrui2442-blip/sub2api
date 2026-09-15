package service

import (
	"errors"
	"strings"
)

const (
	ConsumerWebCredentialNamespace = "consumer_web"
	ConsumerWebPSIDCiphertextKey   = "__Secure-1PSID_ciphertext"
	ConsumerWebPSIDTSCiphertextKey = "__Secure-1PSIDTS_ciphertext"
)

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
	patch := make(map[string]any, 2)
	for key, value := range map[string]*string{
		ConsumerWebPSIDCiphertextKey:   psid,
		ConsumerWebPSIDTSCiphertextKey: psidts,
	} {
		if value == nil || strings.TrimSpace(*value) == "" {
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
