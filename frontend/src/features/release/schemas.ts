import { z } from "zod";

export const releasePrepareSchema = z.object({
  targetIp: z.string().min(1, "IPv4 address is required"),
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

export type ReleasePrepareValues = z.infer<typeof releasePrepareSchema>;
