package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPersonalAPIKeyServiceUsesLocalGroupPermissionsWithoutSubscriptions(t *testing.T) {
	svc := NewPersonalAPIKeyService(nil, nil, nil, nil, nil, nil)
	user := &User{ID: 7, AllowedGroups: []int64{42}}

	allowed := &Group{ID: 42, IsExclusive: true, SubscriptionType: SubscriptionTypeSubscription}
	require.True(t, svc.canUserBindGroup(context.Background(), user, allowed))

	denied := &Group{ID: 43, IsExclusive: true, SubscriptionType: SubscriptionTypeSubscription}
	require.False(t, svc.canUserBindGroup(context.Background(), user, denied))

	public := &Group{ID: 44, IsExclusive: false, SubscriptionType: SubscriptionTypeSubscription}
	require.True(t, svc.canUserBindGroup(context.Background(), user, public))
}
