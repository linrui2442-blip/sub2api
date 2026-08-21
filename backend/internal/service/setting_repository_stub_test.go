//go:build unit

package service

import "context"

// settingRepoStub provides deterministic settings reads for authentication and
// owner-management unit tests without restoring any backup or billing fixture.
type settingRepoStub struct {
	values map[string]string
	err    error
}

func (s *settingRepoStub) Get(_ context.Context, key string) (*Setting, error) {
	if s.err != nil {
		return nil, s.err
	}
	value, ok := s.values[key]
	if !ok {
		return nil, ErrSettingNotFound
	}
	return &Setting{Key: key, Value: value}, nil
}

func (s *settingRepoStub) GetValue(_ context.Context, key string) (string, error) {
	if s.err != nil {
		return "", s.err
	}
	return s.values[key], nil
}

func (s *settingRepoStub) Set(_ context.Context, key, value string) error {
	if s.err != nil {
		return s.err
	}
	if s.values == nil {
		s.values = make(map[string]string)
	}
	s.values[key] = value
	return nil
}

func (s *settingRepoStub) GetMultiple(_ context.Context, keys []string) (map[string]string, error) {
	if s.err != nil {
		return nil, s.err
	}
	result := make(map[string]string)
	for _, key := range keys {
		if value, ok := s.values[key]; ok {
			result[key] = value
		}
	}
	return result, nil
}

func (s *settingRepoStub) SetMultiple(ctx context.Context, values map[string]string) error {
	for key, value := range values {
		if err := s.Set(ctx, key, value); err != nil {
			return err
		}
	}
	return nil
}

func (s *settingRepoStub) GetAll(_ context.Context) (map[string]string, error) {
	if s.err != nil {
		return nil, s.err
	}
	result := make(map[string]string, len(s.values))
	for key, value := range s.values {
		result[key] = value
	}
	return result, nil
}

func (s *settingRepoStub) Delete(_ context.Context, key string) error {
	if s.err != nil {
		return s.err
	}
	delete(s.values, key)
	return nil
}
