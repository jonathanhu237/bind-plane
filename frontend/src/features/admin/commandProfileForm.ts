import { z } from "zod";

import type { CommandProfile } from "@/api/types";
import { pretty } from "@/lib/utils";

export const defaultCommandProfileForm = {
  name: "",
  description: "",
  loginPromptPatterns: pretty({
    username_pattern: "Username:",
    password_pattern: "Password:",
  }),
  commandTemplates: pretty({
    single_arp_query: "display arp $ip",
    config: "system-view",
    arp_release: "undo arp static $ip",
    exit_config: "return",
  }),
  promptPatterns: pretty({
    connection_options: {
      device_type: "hp_comware_telnet",
    },
    query_expect_string: "[>#]",
    release_expect_string: "[>#]",
  }),
  paginationRules: pretty({
    disable_paging_command: "screen-length disable",
  }),
  successPatterns: pretty([]),
  errorPatterns: pretty(["Error", "Invalid"]),
  parserRules: pretty({
    arp_entry_regex: "(?P<ip>\\S+)\\s+(?P<mac>[0-9a-f-]+).*(?P<type>S|D)",
    static_type_values: ["S"],
    dynamic_type_values: ["D"],
    missing_patterns: ["not found", "No matching"],
  }),
  isActive: true,
};

function jsonObject(
  value: string,
  ctx: z.RefinementCtx,
  label: string,
  t: (key: string, options?: Record<string, unknown>) => string,
): Record<string, unknown> {
  try {
    const parsed = JSON.parse(value.trim() || "{}") as unknown;
    if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) {
      throw new Error("not object");
    }
    return parsed as Record<string, unknown>;
  } catch {
    ctx.addIssue({
      code: "custom",
      message: t("validation.jsonObject", { label }),
    });
    return {};
  }
}

function jsonStringArray(
  value: string,
  ctx: z.RefinementCtx,
  label: string,
  t: (key: string, options?: Record<string, unknown>) => string,
): string[] {
  try {
    const parsed = JSON.parse(value.trim() || "[]") as unknown;
    if (!Array.isArray(parsed) || parsed.some((item) => typeof item !== "string")) {
      throw new Error("not string array");
    }
    return parsed;
  } catch {
    ctx.addIssue({
      code: "custom",
      message: t("validation.jsonStringArray", { label }),
    });
    return [];
  }
}

export function createCommandProfileSchema(
  t: (key: string, options?: Record<string, unknown>) => string,
) {
  return z
    .object({
      name: z.string().min(1, t("validation.profileNameRequired")),
      description: z.string(),
      loginPromptPatterns: z.string(),
      commandTemplates: z.string(),
      promptPatterns: z.string(),
      paginationRules: z.string(),
      successPatterns: z.string(),
      errorPatterns: z.string(),
      parserRules: z.string(),
      isActive: z.boolean(),
    })
    .transform((value, ctx) => ({
      name: value.name,
      description: value.description || null,
      login_prompt_patterns: jsonObject(
        value.loginPromptPatterns,
        ctx,
        t("admin.profiles.loginPromptPatterns"),
        t,
      ),
      command_templates: jsonObject(
        value.commandTemplates,
        ctx,
        t("admin.profiles.commandTemplates"),
        t,
      ),
      prompt_patterns: jsonObject(
        value.promptPatterns,
        ctx,
        t("admin.profiles.promptPatterns"),
        t,
      ),
      pagination_rules: jsonObject(
        value.paginationRules,
        ctx,
        t("admin.profiles.paginationRules"),
        t,
      ),
      success_patterns: jsonStringArray(
        value.successPatterns,
        ctx,
        t("admin.profiles.successPatterns"),
        t,
      ),
      error_patterns: jsonStringArray(
        value.errorPatterns,
        ctx,
        t("admin.profiles.errorPatterns"),
        t,
      ),
      parser_rules: jsonObject(
        value.parserRules,
        ctx,
        t("admin.profiles.parserRules"),
        t,
      ),
      is_active: value.isActive,
    }));
}

export type CommandProfileFormValues = typeof defaultCommandProfileForm;
export type CommandProfilePayload = z.output<
  ReturnType<typeof createCommandProfileSchema>
>;

export function commandProfileToForm(profile: CommandProfile): CommandProfileFormValues {
  return {
    name: profile.name,
    description: profile.description ?? "",
    loginPromptPatterns: pretty(profile.login_prompt_patterns),
    commandTemplates: pretty(profile.command_templates),
    promptPatterns: pretty(profile.prompt_patterns),
    paginationRules: pretty(profile.pagination_rules),
    successPatterns: pretty(profile.success_patterns),
    errorPatterns: pretty(profile.error_patterns),
    parserRules: pretty(profile.parser_rules),
    isActive: profile.is_active,
  };
}
