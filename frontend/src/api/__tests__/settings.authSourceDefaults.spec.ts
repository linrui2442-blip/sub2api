import { describe, expect, it } from "vitest";

import {
  appendAuthSourceDefaultsToUpdateRequest,
  buildAuthSourceDefaultsState,
  type UpdateSettingsRequest,
} from "@/api/admin/settings";

describe("admin settings auth source defaults helpers", () => {
  it("builds auth source defaults state from flat settings fields", () => {
    const state = buildAuthSourceDefaultsState({
      auth_source_default_email_balance: 9.5,
      auth_source_default_email_concurrency: 3,
      auth_source_default_email_subscriptions: [{ group_id: 1, validity_days: 30 }],
      auth_source_default_email_grant_on_signup: false,
      auth_source_default_email_grant_on_first_bind: true,
    });

    expect(state.email).toEqual({
      balance: 9.5,
      concurrency: 3,
      subscriptions: [{ group_id: 1, validity_days: 30 }],
      grant_on_signup: false,
      grant_on_first_bind: true,
    });
    expect(state.oidc).toEqual({
      balance: 0,
      concurrency: 5,
      subscriptions: [],
      grant_on_signup: false,
      grant_on_first_bind: false,
    });
  });

  it("appends trusted-member defaults without SaaS quota fields", () => {
    const payload: UpdateSettingsRequest = { site_name: "Sub2API" };
    const defaults = buildAuthSourceDefaultsState({});
    defaults.email = {
      balance: 1.25,
      concurrency: 2,
      subscriptions: [{ group_id: 3, validity_days: 7 }],
      grant_on_signup: true,
      grant_on_first_bind: false,
    };

    appendAuthSourceDefaultsToUpdateRequest(payload, defaults);

    expect(payload).toMatchObject({
      site_name: "Sub2API",
      auth_source_default_email_balance: 1.25,
      auth_source_default_email_concurrency: 2,
      auth_source_default_email_subscriptions: [{ group_id: 3, validity_days: 7 }],
      auth_source_default_email_grant_on_signup: true,
      auth_source_default_email_grant_on_first_bind: false,
    });
    expect(payload).not.toHaveProperty("auth_source_default_email_platform_quotas");
  });
});
