package service

import (
	"context"
	"encoding/json"
	"github.com/stretchr/testify/require"
	"net/http"
	"testing"
	"time"
)

func TestConsumerWebAuxiliaryInvalidAndExpiry(t *testing.T) {
	tr := NewConsumerWebHTTPTransport(ConsumerWebHTTPTransportOptions{Encryptor: consumerWebTransportEncryptor{}})
	entries := []consumerWebAuxEntry{
		{Source: consumerWebAppURL, Cookie: http.Cookie{Name: "expired", Value: "x", Expires: time.Now().Add(-time.Hour)}},
		{Source: "https://evil.example/app", Cookie: http.Cookie{Name: "foreign", Value: "x"}},
		{Source: consumerWebAppURL, Cookie: http.Cookie{Name: "__Secure-1PSID", Value: "bad"}},
		{Source: consumerWebAppURL, Cookie: http.Cookie{Name: "foreign_domain", Value: "x", Domain: "evil.example"}},
		{Source: consumerWebAppURL, Cookie: http.Cookie{Name: "valid_aux", Value: "good", Path: "/", Secure: true}},
	}
	enc := consumerWebTransportEncryptor{}
	values := []string{"malformed"}
	for _, e := range entries {
		b, _ := json.Marshal(e)
		v, _ := enc.Encrypt(string(b))
		values = append(values, v)
	}
	h := tr.newCredentialHandle("original", "original-ts")
	tr.restoreAuxiliary(context.Background(), h, map[string]any{ConsumerWebCredentialNamespace: map[string]any{consumerWebAuxKey: values}})
	got := map[string]string{}
	for _, c := range h.jar.Cookies(mustURL(consumerWebAppURL)) {
		got[c.Name] = c.Value
	}
	require.Equal(t, map[string]string{"__Secure-1PSID": "original", "__Secure-1PSIDTS": "original-ts", "valid_aux": "good"}, got)
}

func TestConsumerWebAuxiliaryDeletionAndMetadata(t *testing.T) {
	tr := NewConsumerWebHTTPTransport(ConsumerWebHTTPTransportOptions{})
	h := tr.newCredentialHandle("p", "ts")
	var saved []consumerWebAuxEntry
	ctx := context.WithValue(context.Background(), consumerWebAuxContextKey{}, consumerWebAuxSave(func(e []consumerWebAuxEntry) { saved = e }))
	tr.restoreAuxiliary(ctx, h, nil)
	mergeConsumerWebAuxiliaryCookies(h.jar, consumerWebAppURL, []*http.Cookie{{Name: "aux", Value: "v", MaxAge: 60, Raw: "SECRET_HEADER", RawExpires: "raw", Unparsed: []string{"raw"}}})
	require.Len(t, saved, 1)
	require.Empty(t, saved[0].Cookie.Raw)
	require.Empty(t, saved[0].Cookie.Unparsed)
	require.Zero(t, saved[0].Cookie.MaxAge)
	require.True(t, saved[0].Cookie.Expires.After(time.Now()))
	mergeConsumerWebAuxiliaryCookies(h.jar, consumerWebAppURL, []*http.Cookie{{Name: "aux", MaxAge: -1}})
	require.Empty(t, saved)
}
