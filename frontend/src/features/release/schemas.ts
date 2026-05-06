import { z } from "zod";

export function createReleasePrepareSchema(t: (key: string) => string) {
  return z.object({
    targetIp: z.string().min(1, t("validation.ipv4Required")),
    reason: z.enum([
      "temporary_test",
      "user_report",
      "ip_mac_change",
      "wrong_binding_fix",
      "security_response",
      "other",
    ]),
    ticketId: z.string(),
    force: z.boolean(),
    selectedSwitchId: z.string(),
  });
}

export type ReleasePrepareValues = z.infer<
  ReturnType<typeof createReleasePrepareSchema>
>;
