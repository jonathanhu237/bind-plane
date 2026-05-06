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

function jsonObject(value: string, ctx: z.RefinementCtx, label: string): Record<string, unknown> {
  try {
    const parsed = JSON.parse(value.trim() || "{}") as unknown;
    if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) {
      throw new Error("not object");
    }
    return parsed as Record<string, unknown>;
  } catch {
    ctx.addIssue({ code: "custom", message: `${label} must be a JSON object` });
    return {};
  }
}

function jsonStringArray(value: string, ctx: z.RefinementCtx, label: string): string[] {
  try {
    const parsed = JSON.parse(value.trim() || "[]") as unknown;
    if (!Array.isArray(parsed) || parsed.some((item) => typeof item !== "string")) {
      throw new Error("not string array");
    }
    return parsed;
  } catch {
    ctx.addIssue({ code: "custom", message: `${label} must be a JSON array of strings` });
    return [];
  }
}

export const commandProfileSchema = z
  .object({
    name: z.string().min(1, "Profile name is required"),
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
    login_prompt_patterns: jsonObject(value.loginPromptPatterns, ctx, "Login prompt patterns"),
    command_templates: jsonObject(value.commandTemplates, ctx, "Command templates"),
    prompt_patterns: jsonObject(value.promptPatterns, ctx, "Prompt patterns"),
    pagination_rules: jsonObject(value.paginationRules, ctx, "Pagination rules"),
    success_patterns: jsonStringArray(value.successPatterns, ctx, "Success patterns"),
    error_patterns: jsonStringArray(value.errorPatterns, ctx, "Error patterns"),
    parser_rules: jsonObject(value.parserRules, ctx, "Parser rules"),
    is_active: value.isActive,
  }));

export type CommandProfileFormValues = typeof defaultCommandProfileForm;
export type CommandProfilePayload = z.output<typeof commandProfileSchema>;

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
