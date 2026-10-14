package service

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"time"
)

const consumerWebAuxKey = "auxiliary_cookie_snapshot"

type consumerWebAuxEntry struct {
	Source string
	Cookie http.Cookie
}
type consumerWebAuxContextKey struct{}
type consumerWebAuxSave func([]consumerWebAuxEntry)
type consumerWebAuxPersister interface {
	PersistAuxiliary(context.Context, int64, any, []consumerWebAuxEntry) (*AccountCredentialSnapshot, error)
}

func (p *conditionalConsumerWebPSIDTSPersister) PersistAuxiliary(ctx context.Context, id int64, rev any, entries []consumerWebAuxEntry) (*AccountCredentialSnapshot, error) {
	if p == nil || p.encryptor == nil {
		return nil, ErrConsumerWebPersistence
	}
	encrypted := make([]string, 0, len(entries))
	for _, entry := range entries {
		if isConsumerWebCoreCookie(entry.Cookie.Name) {
			continue
		}
		b, err := json.Marshal(entry)
		if err != nil {
			return nil, ErrConsumerWebPersistence
		}
		v, err := p.encryptor.Encrypt(string(b))
		if err != nil {
			return nil, ErrConsumerWebPersistence
		}
		encrypted = append(encrypted, v)
	}
	return MutateAccountCredentialsConditional(ctx, p.repo, id, rev, func(current map[string]any) (map[string]any, error) {
		return UpdateAccountCredentialNamespace(current, ConsumerWebCredentialNamespace, map[string]any{consumerWebAuxKey: encrypted}), nil
	})
}

func isConsumerWebCoreCookie(name string) bool {
	return name == "__Secure-1PSID" || name == "__Secure-1PSIDTS"
}

// The wrapper delegates acceptance to the unchanged standard jar. The metadata
// ledger retains expiry/path/host-only semantics which Cookies() cannot export.
// It is only installed on Bootstrap's private, sequentially populated jar.
type consumerWebAuxJar struct {
	http.CookieJar
	entries []consumerWebAuxEntry
	allowed map[string]bool
	save    consumerWebAuxSave
}

func auxIdentity(e consumerWebAuxEntry) string {
	u, _ := url.Parse(e.Source)
	d := strings.TrimPrefix(strings.ToLower(e.Cookie.Domain), ".")
	if d == "" {
		d = u.Hostname()
	}
	return d + "\x00" + e.Cookie.Path + "\x00" + e.Cookie.Name
}

func normalizeAux(u *url.URL, c http.Cookie) (http.Cookie, bool) {
	if u == nil || isConsumerWebCoreCookie(c.Name) || c.Name == "" {
		return c, false
	}
	if c.Path == "" || c.Path[0] != '/' {
		c.Path = "/"
		if i := strings.LastIndex(u.Path, "/"); i > 0 {
			c.Path = u.Path[:i]
		}
	}
	// Check domain acceptance without allowing expiry/deletion to mask rejection.
	probe, _ := cookiejar.New(nil)
	test := c
	test.MaxAge = 0
	test.Expires = time.Time{}
	probe.SetCookies(u, []*http.Cookie{&test})
	check := *u
	check.Path = c.Path
	if c.Secure {
		check.Scheme = "https"
	}
	accepted := false
	for _, got := range probe.Cookies(&check) {
		if got.Name == c.Name && got.Value == c.Value {
			accepted = true
		}
	}
	if !accepted {
		return c, false
	}
	c.Raw = ""
	c.RawExpires = ""
	c.Unparsed = nil
	if c.MaxAge > 0 {
		c.Expires = time.Now().Add(time.Duration(c.MaxAge) * time.Second)
		c.MaxAge = 0
	}
	return c, true
}

func (j *consumerWebAuxJar) SetCookies(u *url.URL, cookies []*http.Cookie) {
	j.CookieJar.SetCookies(u, cookies)
	if u == nil || !j.allowed[u.String()] {
		return
	}
	changed := false
	for _, c := range cookies {
		if c == nil {
			continue
		}
		normalized, ok := normalizeAux(u, *c)
		if !ok {
			continue
		}
		e := consumerWebAuxEntry{Source: u.String(), Cookie: normalized}
		id := auxIdentity(e)
		kept := j.entries[:0]
		for _, old := range j.entries {
			if auxIdentity(old) != id && (old.Cookie.Expires.IsZero() || old.Cookie.Expires.After(time.Now())) {
				kept = append(kept, old)
			}
		}
		j.entries = kept
		if normalized.MaxAge >= 0 && (normalized.Expires.IsZero() || normalized.Expires.After(time.Now())) {
			j.entries = append(j.entries, e)
		}
		changed = true
	}
	if changed && j.save != nil {
		j.save(append([]consumerWebAuxEntry(nil), j.entries...))
	}
}

func (t *ConsumerWebHTTPTransport) restoreAuxiliary(ctx context.Context, h *consumerWebHTTPHandle, credentials map[string]any) {
	j := &consumerWebAuxJar{CookieJar: h.jar, allowed: map[string]bool{}}
	for _, source := range []string{t.endpoints.googlePreflight, t.endpoints.geminiPrime, t.endpoints.app} {
		if source != "" {
			j.allowed[source] = true
		}
	}
	ns, _ := credentials[ConsumerWebCredentialNamespace].(map[string]any)
	// Decode only the envelope shape, never stringify or log secret material.
	b, _ := json.Marshal(ns[consumerWebAuxKey])
	var encrypted []string
	_ = json.Unmarshal(b, &encrypted)
	failed := 0
	for _, value := range encrypted {
		if t.encryptor == nil {
			failed++
			continue
		}
		plain, err := t.encryptor.Decrypt(value)
		if err != nil {
			failed++
			continue
		}
		var e consumerWebAuxEntry
		if json.Unmarshal([]byte(plain), &e) != nil || !j.allowed[e.Source] {
			failed++
			continue
		}
		u, err := url.Parse(e.Source)
		if err != nil {
			failed++
			continue
		}
		c, ok := normalizeAux(u, e.Cookie)
		if !ok || c.MaxAge < 0 || (!c.Expires.IsZero() && !c.Expires.After(time.Now())) {
			continue
		}
		j.SetCookies(u, []*http.Cookie{&c})
	}
	j.save, _ = ctx.Value(consumerWebAuxContextKey{}).(consumerWebAuxSave)
	h.jar = j
	slog.Info("consumer_web_aux_restore", "aux_cookie_restore_count", len(j.entries), "decrypt_failure_count", failed)
}

func (m *ConsumerWebSessionManager) auxiliaryContext(ctx context.Context, account *Account, snapshot **AccountCredentialSnapshot) context.Context {
	p, ok := m.persister.(consumerWebAuxPersister)
	if !ok {
		return ctx
	}
	blocked := false
	return context.WithValue(ctx, consumerWebAuxContextKey{}, consumerWebAuxSave(func(entries []consumerWebAuxEntry) {
		if blocked {
			return
		}
		next, err := p.PersistAuxiliary(ctx, account.ID, (*snapshot).Revision, entries)
		if err == nil {
			*snapshot = next
		} else {
			blocked = true
		}
		// No stale replay: preserve the winner and stop further aux writes on conflict.
		// Core rotation keeps its existing revision/CAS rules.
		slog.Info("consumer_web_aux_persist", "aux_cookie_update_detected", true, "aux_cookie_count", len(entries), "aux_cookie_persisted", err == nil, "cas_conflict", errors.Is(err, ErrCredentialCASConflict))
	}))
}
