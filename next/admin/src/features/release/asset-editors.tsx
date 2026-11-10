import { CircleAlert, Copy, Pencil, Power, PowerOff } from 'lucide-react'
import { useState } from 'react'
import { Alert, AlertDescription } from '@/components/ui/alert'
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { Dialog, DialogClose, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Field, FieldDescription, FieldGroup, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Select, SelectContent, SelectGroup, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Textarea } from '@/components/ui/textarea'
import { cn } from '@/lib/utils'
import { errorText } from './messages'
import { useRequiredFields, type RequiredField } from './required-fields'

export const profileRuleKeys = ['command_templates', 'parser_rules', 'login_prompt_patterns', 'prompt_patterns', 'pagination_rules', 'success_patterns', 'error_patterns', 'snmp_rules'] as const
type RuleKey = (typeof profileRuleKeys)[number]
const arrayRules: ReadonlySet<string> = new Set(['success_patterns', 'error_patterns'])

// The JSON parts of a command profile, named for people; the keys stay what the API stores.
const ruleText: Record<RuleKey, { label: [string, string]; hint: [string, string] }> = {
  command_templates: {
    label: ['Commands', '命令'],
    hint: [
      'JSON object: single_arp_query and arp_release for releases ($ip is the address), optional config and exit_config, and config_backup for backups.',
      'JSON 对象：single_arp_query 和 arp_release 用于解绑（$ip 代表目标地址），config、exit_config 可选，config_backup 用于配置备份。',
    ],
  },
  parser_rules: {
    label: ['Parsing rules', '解析规则'],
    hint: [
      'JSON object: arp_entry_regex with ip, mac and type groups, plus static_type_values, dynamic_type_values and missing_patterns. Where types cannot be listed, such as ages in minutes, static_type_pattern and dynamic_type_pattern take a regular expression.',
      'JSON 对象：arp_entry_regex（含 ip、mac、type 分组），以及 static_type_values、dynamic_type_values、missing_patterns。类型无法逐个列出时（比如用分钟数表示的老化时间），可用 static_type_pattern、dynamic_type_pattern 写正则。',
    ],
  },
  login_prompt_patterns: { label: ['Login prompts', '登录提示符'], hint: ['JSON object of login prompt patterns; {} keeps the driver defaults.', 'JSON 对象，登录提示符的匹配规则；{} 表示沿用驱动默认值。'] },
  prompt_patterns: { label: ['Command prompts', '命令提示符'], hint: ['JSON object of command prompt patterns; {} keeps the driver defaults.', 'JSON 对象，命令提示符的匹配规则；{} 表示沿用驱动默认值。'] },
  pagination_rules: {
    label: ['Paging', '分页处理'],
    hint: ['JSON object; disable_paging_command turns paging off, such as "screen-length 0 temporary". {} for none.', 'JSON 对象；disable_paging_command 用于关闭分页，例如 "screen-length 0 temporary"。{} 表示不处理。'],
  },
  success_patterns: { label: ['Success patterns', '成功匹配'], hint: ['JSON array of regular expressions; release output matching one counts as success.', 'JSON 数组，解绑输出命中其中任一正则即视为成功。'] },
  error_patterns: { label: ['Error patterns', '错误匹配'], hint: ['JSON array of regular expressions; command output matching one counts as an error.', 'JSON 数组，命令输出命中其中任一正则即视为出错。'] },
  snmp_rules: {
    label: ['SNMP metrics', 'SNMP 指标'],
    hint: [
      'JSON object for monitoring: cpu, memory and temperature, each a list of OIDs tried in order; the highest value under the first OID that gives one is the reading. {} reads only standard MIBs (uptime and ports).',
      'JSON 对象，用于设备监控：cpu、memory、temperature 各列出若干 OID，按顺序尝试，取第一个有值的 OID 下的最大值。{} 表示只读标准 MIB（运行时长和端口）。',
    ],
  },
}
export const ruleLabel = (key: string, zh: boolean) => ruleText[key as RuleKey]?.label[zh ? 1 : 0] ?? key
export const ruleHint = (key: string, zh: boolean) => ruleText[key as RuleKey]?.hint[zh ? 1 : 0] ?? ''

// Radix select items need a non-empty value, so "no group" travels as this and is stored as ''.
export const NO_GROUP = 'none'

export type Credential = { id: string; name: string; username: string; description: string | null; is_active: boolean; has_secret: boolean; version: number }
/** A credential for monitoring over SNMP: a v2c community, or a v3 user. Secrets are never listed. */
export type SnmpCredential = {
  id: string; name: string; snmp_version: 'v2c' | 'v3'; username: string | null; auth_protocol: string | null; priv_protocol: string | null
  description: string | null; is_active: boolean; version: number
}
export const snmpAuthProtocols = ['md5', 'sha', 'sha224', 'sha256', 'sha384', 'sha512'] as const
export const snmpPrivProtocols = ['des', 'aes', 'aes192', 'aes256'] as const
export const snmpProtocolName = (protocol: string) => ({ md5: 'MD5', sha: 'SHA-1', sha224: 'SHA-224', sha256: 'SHA-256', sha384: 'SHA-384', sha512: 'SHA-512', des: 'DES', aes: 'AES-128', aes192: 'AES-192', aes256: 'AES-256' })[protocol] ?? protocol
/** Radix select items need a value, so "none" travels as this and is sent as ''. */
export const NONE = 'none'
export type Profile = { id: string; name: string; vendor: Vendor; is_active: boolean; version: number } & Record<RuleKey, unknown>
export type Vendor = 'huawei' | 'h3c' | 'ruijie' | 'cisco' | 'other'
export type SwitchAsset = {
  id: string; name: string; management_ip: string; credential_id: string; command_profile_id: string; is_enabled: boolean; version: number
  vendor: Vendor; model: string | null; site: string | null; description: string | null; group_id: string | null; protocol: 'telnet' | 'ssh'; port: number | null
  /** A release switch is the gateway of its users' networks, which are read from its configuration. */
  handles_release: boolean; networks_succeeded_at: string | null; networks_error: string | null
  /** A switch with an SNMP credential is monitored; without a port of its own it uses 161. */
  snmp_credential_id: string | null; snmp_port: number | null
}
export type GroupAsset = { id: string; name: string; description: string | null; version: number }
/** A network read from a release switch's configuration; one without an interface was typed in before that. */
export type NetworkAsset = { id: string; switch_id: string; cidr: string; is_enabled: boolean; is_validated: boolean; vlan: string | null; description: string | null; interface: string | null; version: number }
export type Inventory = { credentials: Credential[]; snmp_credentials: SnmpCredential[]; profiles: Profile[]; switches: SwitchAsset[]; networks: NetworkAsset[]; groups: GroupAsset[] }
export type AssetKind = 'credentials' | 'snmp_credentials' | 'profiles' | 'switches' | 'groups'
/** The API path of an asset kind. */
export const assetPath = (kind: AssetKind) => (kind === 'snmp_credentials' ? 'snmp-credentials' : kind)
export const vendors: Vendor[] = ['huawei', 'h3c', 'ruijie', 'cisco', 'other']
export type EditTarget =
  | { kind: 'credentials'; item: Credential }
  | { kind: 'snmp_credentials'; item: SnmpCredential }
  | { kind: 'profiles'; item: Profile }
  | { kind: 'switches'; item: SwitchAsset }
  | { kind: 'groups'; item: GroupAsset }


export class AssetRequestError extends Error {
  status: number
  constructor(message: string, status: number) {
    super(message)
    this.status = status
  }
}

export async function callAssets<T>(path: string, body?: unknown, method = body === undefined ? 'GET' : 'POST'): Promise<T> {
  const response = await fetch(`/api/releases/admin/${path}`, {
    method, credentials: 'same-origin', cache: 'no-store',
    headers: { Accept: 'application/json', ...(body === undefined ? {} : { 'Content-Type': 'application/json' }) },
    body: body === undefined ? undefined : JSON.stringify(body),
  })
  const data: unknown = await response.json().catch(() => ({}))
  if (!response.ok) throw new AssetRequestError((data as { detail?: string }).detail || `HTTP ${response.status}`, response.status)
  return data as T
}

export function isOn(target: EditTarget): boolean {
  if (target.kind === 'groups') return true
  return target.kind === 'switches' ? target.item.is_enabled : target.item.is_active
}

export function statusBody(target: EditTarget, on: boolean): Record<string, unknown> {
  return { version: target.item.version, [target.kind === 'switches' ? 'is_enabled' : 'is_active']: on }
}

export function assetName(target: EditTarget): string {
  return target.item.name
}

/** The port a login protocol uses unless the switch says otherwise. */
export const defaultPort = (protocol: string) => (protocol === 'ssh' ? '22' : '23')

/** Switching protocol moves a port that was empty or the old protocol's default to the new protocol's. */
export function portFollowing(current: Record<string, unknown>, key: string, next: unknown): { port?: string } {
  if (key !== 'protocol' || typeof next !== 'string') return {}
  const port = String(current.port ?? '').trim()
  return !port || port === defaultPort(String(current.protocol)) ? { port: defaultPort(next) } : {}
}

/** The command profiles a switch of this vendor can use: a profile speaks one vendor's commands. */
export const vendorProfiles = (profiles: Profile[] | undefined, vendor: string) => (profiles ?? []).filter((profile) => profile.vendor === vendor)

/** A new vendor keeps a profile that is the vendor's, or else takes the vendor's only active profile. */
export function profileFollowing(current: Record<string, unknown>, key: string, next: unknown, profiles?: Profile[]): { command_profile_id?: string } {
  if (key !== 'vendor' || typeof next !== 'string') return {}
  const own = vendorProfiles(profiles, next)
  if (own.some((profile) => profile.id === current.command_profile_id)) return {}
  const active = own.filter((profile) => profile.is_active)
  return { command_profile_id: active.length === 1 ? active[0].id : '' }
}

export const vendorName = (vendor: string, zh: boolean) =>
  ({ huawei: zh ? '华为' : 'Huawei', h3c: 'H3C', ruijie: zh ? '锐捷' : 'Ruijie', cisco: 'Cisco', other: zh ? '其他' : 'Other' })[vendor] ?? vendor

export function switchesUsing(target: EditTarget, inventory?: Inventory): number {
  if (target.kind === 'credentials') return inventory?.switches.filter((item) => item.credential_id === target.item.id).length ?? 0
  if (target.kind === 'profiles') return inventory?.switches.filter((item) => item.command_profile_id === target.item.id).length ?? 0
  if (target.kind === 'snmp_credentials') return inventory?.switches.filter((item) => item.snmp_credential_id === target.item.id).length ?? 0
  return 0
}

type Form = Record<string, string | boolean>

function initialForm(target: EditTarget): Form {
  switch (target.kind) {
    case 'credentials':
      return { name: target.item.name, username: target.item.username, description: target.item.description ?? '', password: '', secret: '', clear_secret: false }
    case 'snmp_credentials':
      return {
        name: target.item.name, description: target.item.description ?? '', snmp_version: target.item.snmp_version, community: '', username: target.item.username ?? '',
        auth_protocol: target.item.auth_protocol ?? NONE, auth_password: '', priv_protocol: target.item.priv_protocol ?? NONE, priv_password: '',
      }
    case 'profiles':
      return { name: target.item.name, vendor: target.item.vendor ?? 'other', ...Object.fromEntries(profileRuleKeys.map((key) => [key, JSON.stringify(target.item[key] ?? (arrayRules.has(key) ? [] : {}), null, 2)])) }
    case 'switches':
      return {
        name: target.item.name, management_ip: target.item.management_ip, credential_id: target.item.credential_id, command_profile_id: target.item.command_profile_id,
        vendor: target.item.vendor ?? 'other', model: target.item.model ?? '', site: target.item.site ?? '', description: target.item.description ?? '',
        group_id: target.item.group_id ?? '', protocol: target.item.protocol ?? 'telnet', port: target.item.port ? String(target.item.port) : defaultPort(target.item.protocol ?? 'telnet'),
        handles_release: Boolean(target.item.handles_release),
        snmp_credential_id: target.item.snmp_credential_id ?? '', snmp_port: target.item.snmp_port ? String(target.item.snmp_port) : '',
      }
    case 'groups':
      return { name: target.item.name, description: target.item.description ?? '' }
  }
}

/** An SNMP credential's form as the API takes it; empty secrets keep the stored ones. */
export function snmpCredentialBody(form: Record<string, string | boolean>): Record<string, unknown> {
  const text = (key: string) => String(form[key] ?? '')
  const protocol = (key: string) => (text(key) === NONE ? '' : text(key))
  const secret = (key: string) => (text(key) ? { [key]: text(key) } : {})
  const base = { name: text('name'), description: text('description'), snmp_version: text('snmp_version') }
  if (text('snmp_version') === 'v2c') return { ...base, ...secret('community') }
  const auth = protocol('auth_protocol')
  const priv = auth ? protocol('priv_protocol') : ''
  return { ...base, username: text('username'), auth_protocol: auth, priv_protocol: priv, ...(auth ? secret('auth_password') : {}), ...(priv ? secret('priv_password') : {}) }
}

export type EditRequest = { path: string; body: Record<string, unknown> } | { invalidRule: string }

// editRequest sends the full editable state with the version it was loaded at; the API ignores
// unchanged values. Empty password fields keep the stored secrets.
export function editRequest(target: EditTarget, form: Form): EditRequest {
  const text = (key: string) => String(form[key] ?? '')
  const path = `${assetPath(target.kind)}/${target.item.id}`
  const version = target.item.version
  switch (target.kind) {
    case 'credentials':
      return {
        path,
        body: {
          version, name: text('name'), username: text('username'), description: text('description'),
          ...(text('password') ? { password: text('password') } : {}),
          ...(form.clear_secret ? { clear_secret: true } : text('secret') ? { secret: text('secret') } : {}),
        },
      }
    case 'snmp_credentials':
      return { path, body: { version, ...snmpCredentialBody(form) } }
    case 'profiles': {
      const body: Record<string, unknown> = { version, name: text('name'), vendor: text('vendor') }
      for (const key of profileRuleKeys) {
        let parsed: unknown
        try {
          parsed = JSON.parse(text(key))
        } catch {
          return { invalidRule: key }
        }
        if (parsed === null || typeof parsed !== 'object' || Array.isArray(parsed) !== arrayRules.has(key)) return { invalidRule: key }
        body[key] = parsed
      }
      return { path, body }
    }
    case 'switches': {
      const port = text('port').trim()
      if (port && !/^\d+$/.test(port)) return { invalidRule: 'port' }
      const snmpPort = text('snmp_port').trim()
      if (snmpPort && !/^\d+$/.test(snmpPort)) return { invalidRule: 'snmp_port' }
      // A switch that never had a port of its own keeps none while it uses its protocol's default, so it
      // goes on following the protocol; 0 asks the API for that. An empty field means the default too.
      const fallback = Number(defaultPort(text('protocol')))
      const chosen = port ? Number(port) : fallback
      return {
        path,
        body: {
          version, name: text('name'), management_ip: text('management_ip').trim(), credential_id: text('credential_id'), command_profile_id: text('command_profile_id'),
          vendor: text('vendor'), model: text('model'), site: text('site'), description: text('description'), group_id: text('group_id'), protocol: text('protocol'), port: !target.item.port && chosen === fallback ? 0 : chosen,
          handles_release: Boolean(form.handles_release),
          // 0 asks for the standard SNMP port, 161.
          snmp_credential_id: text('snmp_credential_id'), snmp_port: snmpPort ? Number(snmpPort) : 0,
        },
      }
    }
    case 'groups':
      return { path, body: { version, name: text('name'), description: text('description') } }
  }
}

export function editorLabels(zh: boolean) {
  const noun: Record<AssetKind, string> = zh
    ? { credentials: '凭据', snmp_credentials: 'SNMP 凭据', profiles: '命令模板', switches: '交换机', groups: '分组' }
    : { credentials: 'credential', snmp_credentials: 'SNMP credential', profiles: 'command profile', switches: 'switch', groups: 'group' }
  const switchCount = (count: number) => zh ? `${count} 台交换机` : `${count} switch${count === 1 ? '' : 'es'}`
  return zh ? {
    edit: '编辑', copy: '复制', enable: '启用', disable: '停用', disabled: '已停用', cancel: '取消', close: '关闭',
    save: '保存修改', saving: '正在保存…', saved: '已保存。', choose: '请选择', noVendorProfile: '这个厂商还没有命令模板',
    name: '名称', username: '设备用户名', description: '描述（可选）', newPassword: '新设备密码', newSecret: '新特权密码',
    keep: '留空则保留现有值', clearSecret: '删除已保存的特权密码', managementIP: '管理 IPv4', credential: '凭据', profile: '命令模板',
    inactive: '（已停用）', handlesRelease: '负责解绑',
    snmpCredential: 'SNMP 凭据', noSnmp: '不监控', snmpPort: 'SNMP 端口', snmpHint: '选择 SNMP 凭据后，系统按监控设置的间隔采集这台交换机的 CPU、内存、温度和端口状态，只读取不修改。',
    snmpVersion: 'SNMP 版本', community: '团体字（只读）', snmpUser: 'SNMPv3 用户名', authProtocol: '认证方式', authPassword: '认证密码', privProtocol: '加密方式', privPassword: '加密密码',
    noAuth: '不认证', noPriv: '不加密', passwordRule: 'SNMPv3 密码至少 8 个字符。',
    handlesReleaseHint: '网关交换机打开：解绑时按它的网段找到它，网段从它的配置里自动读取。',
    vendor: '厂商', model: '型号（可选）', site: '站点或校区（可选）', group: '分组', noGroup: '不分组', protocol: '登录协议', port: '端口',
    invalidPort: '端口必须是 1–65535 之间的数字。',
    editTitle: (kind: AssetKind) => `编辑${noun[kind]}`,
    hint: {
      credentials: '密码不会回显。两个密码框留空即保留现有值。',
      snmp_credentials: '团体字和密码不会回显，留空即保留现有值。保存后，使用它的交换机下一次采集就会用新的设置。',
      profiles: '保存会改变此模板发送的命令。使用它的交换机上排队中的解绑会直接失败、不会连接交换机，需要重新解绑。',
      switches: '保存后，此交换机上排队中的解绑会直接失败、不会连接交换机，需要重新解绑。',
      groups: '分组用于筛选设备、限定计划任务和合规规则的范围。',
    } satisfies Record<AssetKind, string>,
    invalidRule: (key: string) => key === 'port' || key === 'snmp_port' ? '端口必须是 1–65535 之间的数字。' : `「${ruleLabel(key, true)}」必须是合法的 JSON ${arrayRules.has(key) ? '数组' : '对象'}。`,
    usedBy: (count: number) => count ? `${switchCount(count)}在用` : '暂无交换机使用',
    ownedBy: (name: string) => `交换机 ${name}`,
    disableTitle: (target: EditTarget) => `停用${noun[target.kind]}「${assetName(target)}」？`,
    disableImpact: (target: EditTarget, inventory?: Inventory) => {
      const count = switchesUsing(target, inventory)
      switch (target.kind) {
        case 'credentials':
        case 'profiles':
          return count ? `${switchCount(count)}使用此${noun[target.kind]}。重新启用之前，它们不参与解析，排队中的任务会直接失败，不会连接交换机。` : `目前没有交换机使用此${noun[target.kind]}。`
        case 'snmp_credentials':
          return count ? `${switchCount(count)}使用此 SNMP 凭据。重新启用之前，它们停止监控，监控告警会自动解决。` : '目前没有交换机使用此 SNMP 凭据。'
        case 'switches':
          return '其网段内的地址将不再解析到此交换机。排队中的任务会直接失败，不会连接交换机。'
        case 'groups':
          return ''
      }
    },
    statusChanged: (target: EditTarget, on: boolean) => `${noun[target.kind]}「${assetName(target)}」已${on ? '启用' : '停用'}。`,
    empty: {
      credentials: '还没有凭据。添加交换机之前需要先保存凭据。',
      snmp_credentials: '还没有 SNMP 凭据。新建一个并分配给交换机后，系统就会开始监控它们。',
      profiles: '还没有命令模板。添加交换机之前需要先保存模板。',
      switches: '还没有交换机。',
      groups: '还没有分组。导入交换机时也会自动创建分组。',
    } satisfies Record<AssetKind, string>,
  } : {
    edit: 'Edit', copy: 'Copy', enable: 'Enable', disable: 'Disable', disabled: 'Disabled', cancel: 'Cancel', close: 'Close',
    save: 'Save changes', saving: 'Saving…', saved: 'Saved.', choose: 'Choose…', noVendorProfile: 'No command profile for this vendor yet',
    name: 'Name', username: 'Device username', description: 'Description (optional)', newPassword: 'New device password', newSecret: 'New privilege secret',
    keep: 'Leave empty to keep the stored value', clearSecret: 'Remove the stored privilege secret', managementIP: 'Management IPv4', credential: 'Credential', profile: 'Command profile',
    inactive: ' (disabled)', handlesRelease: 'Handles releases',
    snmpCredential: 'SNMP credential', noSnmp: 'Not monitored', snmpPort: 'SNMP port', snmpHint: 'With an SNMP credential, the switch is polled for CPU, memory, temperature and ports at the monitoring interval. Polls only read.',
    snmpVersion: 'SNMP version', community: 'Community (read-only)', snmpUser: 'SNMPv3 user', authProtocol: 'Authentication', authPassword: 'Authentication password', privProtocol: 'Privacy', privPassword: 'Privacy password',
    noAuth: 'No authentication', noPriv: 'No privacy', passwordRule: 'SNMPv3 passwords are at least 8 characters.',
    handlesReleaseHint: 'For gateway switches: releases find it by its networks, which are read from its configuration.',
    vendor: 'Vendor', model: 'Model (optional)', site: 'Site or campus (optional)', group: 'Group', noGroup: 'No group', protocol: 'Login protocol', port: 'Port',
    invalidPort: 'Port must be a number from 1 to 65535.',
    editTitle: (kind: AssetKind) => `Edit ${noun[kind]}`,
    hint: {
      credentials: 'Stored passwords are never shown. Leave both password fields empty to keep them.',
      snmp_credentials: 'Communities and passwords are never shown; leave them empty to keep them. Switches using it poll with the new settings next time.',
      profiles: 'Saving changes the commands this profile sends. Releases queued on its switches fail without contacting the switch and must be started again.',
      switches: 'After saving, releases queued on this switch fail without contacting it and must be started again.',
      groups: 'Groups filter devices and scope schedules and compliance rules.',
    } satisfies Record<AssetKind, string>,
    invalidRule: (key: string) => key === 'port' || key === 'snmp_port' ? 'Port must be a number from 1 to 65535.' : `${ruleLabel(key, false)} must be a valid JSON ${arrayRules.has(key) ? 'array' : 'object'}.`,
    usedBy: (count: number) => count ? `Used by ${switchCount(count)}` : 'Not used by any switch',
    ownedBy: (name: string) => `Switch ${name}`,
    disableTitle: (target: EditTarget) => `Disable ${noun[target.kind]} “${assetName(target)}”?`,
    disableImpact: (target: EditTarget, inventory?: Inventory) => {
      const count = switchesUsing(target, inventory)
      switch (target.kind) {
        case 'credentials':
        case 'profiles':
          return count ? `${switchCount(count)} use${count === 1 ? 's' : ''} this ${noun[target.kind]}. Until it is enabled again they stop resolving, and their queued jobs fail without contacting the switch.` : `No switch uses this ${noun[target.kind]}.`
        case 'snmp_credentials':
          return count ? `${switchCount(count)} use${count === 1 ? 's' : ''} this SNMP credential. Until it is enabled again they are not monitored, and their monitoring alerts resolve.` : 'No switch uses this SNMP credential.'
        case 'switches':
          return 'Addresses in its networks stop resolving to it. Its queued jobs fail without contacting the switch.'
        case 'groups':
          return ''
      }
    },
    statusChanged: (target: EditTarget, on: boolean) => `${noun[target.kind][0].toUpperCase()}${noun[target.kind].slice(1)} “${assetName(target)}” ${on ? 'enabled' : 'disabled'}.`,
    empty: {
      credentials: 'No credentials yet. Switches need one before they can be added.',
      snmp_credentials: 'No SNMP credentials yet. Create one and give it to switches to start monitoring them.',
      profiles: 'No command profiles yet. Switches need one before they can be added.',
      switches: 'No switches yet.',
      groups: 'No groups yet. Importing switches creates their groups too.',
    } satisfies Record<AssetKind, string>,
  }
}
export type EditorLabels = ReturnType<typeof editorLabels>

export function AssetRow({ title, detail, on, flag, mono = false, busy, labels, onEdit, onCopy, onToggle }: {
  title: string; detail?: React.ReactNode; on: boolean; flag?: React.ReactNode; mono?: boolean; busy: boolean
  labels: EditorLabels; onEdit?: () => void; onCopy?: () => void; onToggle?: () => void
}) {
  // The 12rem basis lets the actions wrap below the text on narrow screens instead of squeezing it.
  return <li className="flex flex-wrap items-center gap-x-3 gap-y-1 py-2">
    <div className="min-w-0 flex-[1_1_12rem]">
      <div className="flex flex-wrap items-center gap-2 text-sm">
        <span className={cn('break-words font-medium', mono && 'font-mono', !on && 'text-muted-foreground')}>{title}</span>
        {on ? null : <Badge variant="outline" className="font-normal text-muted-foreground">{labels.disabled}</Badge>}
        {flag}
      </div>
      {detail ? <p className="mt-0.5 break-words text-xs text-muted-foreground">{detail}</p> : null}
    </div>
    <div className="ml-auto flex shrink-0 items-center gap-1">
      {onEdit ? <Button type="button" variant="ghost" size="sm" disabled={busy} onClick={onEdit} aria-label={`${labels.edit} ${title}`}><Pencil aria-hidden="true" />{labels.edit}</Button> : null}
      {onCopy ? <Button type="button" variant="ghost" size="sm" disabled={busy} onClick={onCopy} aria-label={`${labels.copy} ${title}`}><Copy aria-hidden="true" />{labels.copy}</Button> : null}
      {onToggle ? <Button type="button" variant="ghost" size="sm" disabled={busy} onClick={onToggle} aria-label={`${on ? labels.disable : labels.enable} ${title}`}>
        {on ? <PowerOff aria-hidden="true" /> : <Power aria-hidden="true" />}{on ? labels.disable : labels.enable}
      </Button> : null}
    </div>
  </li>
}

function EditorForm({ target, inventory, labels, zh, onSave }: {
  target: EditTarget; inventory?: Inventory; labels: EditorLabels; zh: boolean; onSave: (path: string, body: Record<string, unknown>) => Promise<void>
}) {
  const [form, setForm] = useState<Form>(() => initialForm(target))
  const [error, setError] = useState('')
  const [invalidRule, setInvalidRule] = useState('')
  const [busy, setBusy] = useState(false)
  const value = (key: string) => String(form[key] ?? '')
  const need = (id: string, key: string, label: string, choose = false): RequiredField => ({ id, label, value: value(key), choose })
  const required = useRequiredFields(zh, {
    credentials: [need('edit-name', 'name', labels.name), need('edit-username', 'username', labels.username)],
    snmp_credentials: [need('edit-name', 'name', labels.name), ...(value('snmp_version') === 'v3' ? [need('edit-snmp-user', 'username', labels.snmpUser)] : [])],
    profiles: [need('edit-name', 'name', labels.name)],
    switches: [need('edit-name', 'name', labels.name), need('edit-management-ip', 'management_ip', labels.managementIP),
      need('edit-credential', 'credential_id', labels.credential, true), need('edit-profile', 'command_profile_id', labels.profile, true)],
    groups: [need('edit-name', 'name', labels.name)],
  }[target.kind])
  const set = (key: string, next: string | boolean) => setForm((current) => ({
    ...current, [key]: next, ...portFollowing(current, key, next), ...(target.kind === 'switches' ? profileFollowing(current, key, next, inventory?.profiles) : {}),
  }))
  const text = (id: string, key: string, label: string, options: { mono?: boolean; inputMode?: 'decimal' } = {}) =>
    <Field data-invalid={required.invalid(id)}>
      <FieldLabel htmlFor={id}>{label}</FieldLabel>
      <Input id={id} value={value(key)} onChange={(event) => set(key, event.target.value)} {...required.control(id)} inputMode={options.inputMode} autoComplete="off" className={cn(options.mono && 'font-mono')} />
      {required.message(id)}
    </Field>
  const select = (id: string, key: string, label: string, items: { id: string; name: string; on: boolean }[], placeholder = labels.choose) =>
    <Field data-invalid={required.invalid(id)}>
      <FieldLabel htmlFor={id}>{label}</FieldLabel>
      <Select value={value(key)} onValueChange={(next) => set(key, next)}>
        <SelectTrigger id={id} {...required.control(id)}><SelectValue placeholder={placeholder} /></SelectTrigger>
        <SelectContent><SelectGroup>{items.map((item) => <SelectItem key={item.id} value={item.id}>{item.name}{item.on ? '' : labels.inactive}</SelectItem>)}</SelectGroup></SelectContent>
      </Select>
      {required.message(id)}
    </Field>
  const choice = (id: string, key: string, label: string, options: { value: string; label: string }[]) =>
    <Field>
      <FieldLabel htmlFor={id}>{label}</FieldLabel>
      <Select value={value(key)} onValueChange={(next) => set(key, next)}>
        <SelectTrigger id={id}><SelectValue /></SelectTrigger>
        <SelectContent><SelectGroup>{options.map((option) => <SelectItem key={option.value} value={option.value}>{option.label}</SelectItem>)}</SelectGroup></SelectContent>
      </Select>
    </Field>
  const toggle = (id: string, key: string, label: string) =>
    <Field orientation="horizontal">
      <Checkbox id={id} checked={Boolean(form[key])} onCheckedChange={(checked) => set(key, checked === true)} />
      <FieldLabel htmlFor={id} className="font-normal">{label}</FieldLabel>
    </Field>

  async function submit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!required.check()) return
    const request = editRequest(target, form)
    if ('invalidRule' in request) {
      setInvalidRule(request.invalidRule)
      setError(labels.invalidRule(request.invalidRule))
      return
    }
    setBusy(true); setError(''); setInvalidRule('')
    try { await onSave(request.path, request.body) }
    catch (cause) { setError(errorText(cause, zh)) }
    finally { setBusy(false) }
  }

  return <form className="flex flex-col gap-6" noValidate onSubmit={(event) => void submit(event)}>
    <DialogHeader>
      <DialogTitle>{labels.editTitle(target.kind)}</DialogTitle>
      <DialogDescription>{labels.hint[target.kind]}</DialogDescription>
    </DialogHeader>
    <FieldGroup>
      {target.kind === 'credentials' ? <>
        {text('edit-name', 'name', labels.name)}
        {text('edit-username', 'username', labels.username)}
        {text('edit-description', 'description', labels.description)}
        <Field>
          <FieldLabel htmlFor="edit-password">{labels.newPassword}</FieldLabel>
          <Input id="edit-password" type="password" autoComplete="new-password" placeholder={labels.keep} value={value('password')} onChange={(event) => set('password', event.target.value)} />
        </Field>
        <Field data-disabled={Boolean(form.clear_secret)}>
          <FieldLabel htmlFor="edit-secret">{labels.newSecret}</FieldLabel>
          <Input id="edit-secret" type="password" autoComplete="new-password" placeholder={labels.keep} disabled={Boolean(form.clear_secret)} value={value('secret')} onChange={(event) => set('secret', event.target.value)} />
        </Field>
        {target.item.has_secret ? toggle('edit-clear-secret', 'clear_secret', labels.clearSecret) : null}
      </> : null}
      {target.kind === 'snmp_credentials' ? <SnmpCredentialFields idPrefix="edit" form={form} labels={labels} keep onChange={set} required={required} /> : null}
      {target.kind === 'profiles' ? <>
        {text('edit-name', 'name', labels.name)}
        {choice('edit-vendor', 'vendor', labels.vendor, vendors.map((vendor) => ({ value: vendor, label: vendorName(vendor, zh) })))}
        {profileRuleKeys.map((key) => <Field key={key} data-invalid={invalidRule === key}>
          <FieldLabel htmlFor={`edit-${key}`}>{ruleLabel(key, zh)}</FieldLabel>
          <Textarea id={`edit-${key}`} className="font-mono" aria-invalid={invalidRule === key || undefined} aria-describedby={`edit-${key}-hint`} spellCheck={false}
            rows={key === 'parser_rules' || key === 'command_templates' ? 6 : 3} value={value(key)} onChange={(event) => set(key, event.target.value)} />
          <FieldDescription id={`edit-${key}-hint`}>{ruleHint(key, zh)}</FieldDescription>
        </Field>)}
      </> : null}
      {target.kind === 'switches' ? <>
        {text('edit-name', 'name', labels.name)}
        {text('edit-management-ip', 'management_ip', labels.managementIP, { mono: true, inputMode: 'decimal' })}
        <div className="grid gap-4 sm:grid-cols-2">
          {choice('edit-vendor', 'vendor', labels.vendor, vendors.map((vendor) => ({ value: vendor, label: vendorName(vendor, zh) })))}
          {text('edit-model', 'model', labels.model)}
        </div>
        {select('edit-profile', 'command_profile_id', labels.profile, vendorProfiles(inventory?.profiles, String(form.vendor)).map((item) => ({ id: item.id, name: item.name, on: item.is_active })),
          vendorProfiles(inventory?.profiles, String(form.vendor)).length ? labels.choose : labels.noVendorProfile)}
        {select('edit-credential', 'credential_id', labels.credential, (inventory?.credentials ?? []).map((item) => ({ id: item.id, name: item.name, on: item.is_active })))}
        <div className="grid gap-4 sm:grid-cols-2">
          {choice('edit-protocol', 'protocol', labels.protocol, [{ value: 'ssh', label: 'SSH' }, { value: 'telnet', label: 'Telnet' }])}
          <Field data-invalid={invalidRule === 'port'}>
            <FieldLabel htmlFor="edit-port">{labels.port}</FieldLabel>
            <Input id="edit-port" inputMode="numeric" placeholder={defaultPort(String(form.protocol))} aria-invalid={invalidRule === 'port' || undefined}
              value={value('port')} onChange={(event) => set('port', event.target.value)} className="font-mono" />
          </Field>
        </div>
        <Field>
          <FieldLabel htmlFor="edit-group">{labels.group}</FieldLabel>
          <Select value={value('group_id') || NO_GROUP} onValueChange={(next) => set('group_id', next === NO_GROUP ? '' : next)}>
            <SelectTrigger id="edit-group"><SelectValue /></SelectTrigger>
            <SelectContent><SelectGroup>
              <SelectItem value={NO_GROUP}>{labels.noGroup}</SelectItem>
              {(inventory?.groups ?? []).map((group) => <SelectItem key={group.id} value={group.id}>{group.name}</SelectItem>)}
            </SelectGroup></SelectContent>
          </Select>
        </Field>
        {text('edit-site', 'site', labels.site)}
        {text('edit-description', 'description', labels.description)}
        <Field orientation="horizontal">
          <Checkbox id="edit-handles-release" checked={Boolean(form.handles_release)} onCheckedChange={(checked) => set('handles_release', checked === true)} />
          <div className="flex flex-col gap-1">
            <FieldLabel htmlFor="edit-handles-release" className="font-normal">{labels.handlesRelease}</FieldLabel>
            <FieldDescription>{labels.handlesReleaseHint}</FieldDescription>
          </div>
        </Field>
        <SnmpChoice idPrefix="edit" credentialId={value('snmp_credential_id')} port={value('snmp_port')} invalidPort={invalidRule === 'snmp_port'} credentials={inventory?.snmp_credentials} labels={labels}
          onCredential={(next) => set('snmp_credential_id', next)} onPort={(next) => set('snmp_port', next)} />
      </> : null}
      {target.kind === 'groups' ? <>
        {text('edit-name', 'name', labels.name)}
        {text('edit-description', 'description', labels.description)}
      </> : null}
    </FieldGroup>
    {error ? <Alert variant="destructive"><CircleAlert aria-hidden="true" /><AlertDescription>{error}</AlertDescription></Alert> : null}
    <DialogFooter>
      <DialogClose asChild><Button type="button" variant="outline">{labels.cancel}</Button></DialogClose>
      <Button type="submit" disabled={busy}>{busy ? labels.saving : labels.save}</Button>
    </DialogFooter>
  </form>
}

/** The switch's SNMP credential, which turns monitoring on, and its SNMP port. */
export function SnmpChoice({ idPrefix, credentialId, port, invalidPort = false, credentials, labels, onCredential, onPort }: {
  idPrefix: string; credentialId: string; port: string; invalidPort?: boolean; credentials?: SnmpCredential[]; labels: EditorLabels
  onCredential: (id: string) => void; onPort: (port: string) => void
}) {
  return <div className="grid gap-4 sm:grid-cols-[1fr_8rem]">
    <Field>
      <FieldLabel htmlFor={`${idPrefix}-snmp-credential`}>{labels.snmpCredential}</FieldLabel>
      <Select value={credentialId || NONE} onValueChange={(next) => onCredential(next === NONE ? '' : next)}>
        <SelectTrigger id={`${idPrefix}-snmp-credential`} aria-describedby={`${idPrefix}-snmp-hint`}><SelectValue /></SelectTrigger>
        <SelectContent><SelectGroup>
          <SelectItem value={NONE}>{labels.noSnmp}</SelectItem>
          {(credentials ?? []).map((item) => <SelectItem key={item.id} value={item.id}>{item.name}{item.is_active ? '' : labels.inactive}</SelectItem>)}
        </SelectGroup></SelectContent>
      </Select>
    </Field>
    <Field data-invalid={invalidPort} data-disabled={!credentialId}>
      <FieldLabel htmlFor={`${idPrefix}-snmp-port`}>{labels.snmpPort}</FieldLabel>
      <Input id={`${idPrefix}-snmp-port`} inputMode="numeric" placeholder="161" disabled={!credentialId} aria-invalid={invalidPort || undefined}
        value={port} onChange={(event) => onPort(event.target.value)} className="font-mono" />
    </Field>
    <FieldDescription id={`${idPrefix}-snmp-hint`} className="sm:col-span-2">{labels.snmpHint}</FieldDescription>
  </div>
}

/** The fields of an SNMP credential: a community for v2c, or a user and its protocols for v3. */
export function SnmpCredentialFields({ idPrefix, form, labels, keep = false, onChange, required }: {
  idPrefix: string; form: Record<string, string | boolean>; labels: EditorLabels; keep?: boolean
  onChange: (key: string, value: string) => void; required: ReturnType<typeof useRequiredFields>
}) {
  const value = (key: string) => String(form[key] ?? '')
  const id = (key: string) => `${idPrefix}-snmp-${key}`
  const v3 = value('snmp_version') === 'v3'
  const authOn = value('auth_protocol') !== NONE
  const privOn = authOn && value('priv_protocol') !== NONE
  const secret = (key: string, label: string, hint?: string) => <Field data-invalid={required.invalid(id(key))}>
    <FieldLabel htmlFor={id(key)}>{label}</FieldLabel>
    <Input id={id(key)} type="password" autoComplete="new-password" placeholder={keep ? labels.keep : undefined} value={value(key)} onChange={(event) => onChange(key, event.target.value)}
      {...required.control(id(key), hint ? `${id(key)}-hint` : undefined)} />
    {hint ? <FieldDescription id={`${id(key)}-hint`}>{hint}</FieldDescription> : null}
    {required.message(id(key))}
  </Field>
  const choice = (key: string, label: string, options: { value: string; label: string }[], disabled = false) => <Field data-disabled={disabled}>
    <FieldLabel htmlFor={id(key)}>{label}</FieldLabel>
    <Select value={value(key)} onValueChange={(next) => onChange(key, next)} disabled={disabled}>
      <SelectTrigger id={id(key)}><SelectValue /></SelectTrigger>
      <SelectContent><SelectGroup>{options.map((option) => <SelectItem key={option.value} value={option.value}>{option.label}</SelectItem>)}</SelectGroup></SelectContent>
    </Select>
  </Field>
  const nameId = idPrefix === 'edit' ? 'edit-name' : `${idPrefix}-snmp-name`
  const userId = idPrefix === 'edit' ? 'edit-snmp-user' : `${idPrefix}-snmp-user`
  return <>
    <Field data-invalid={required.invalid(nameId)}>
      <FieldLabel htmlFor={nameId}>{labels.name}</FieldLabel>
      <Input id={nameId} value={value('name')} onChange={(event) => onChange('name', event.target.value)} {...required.control(nameId)} autoComplete="off" />
      {required.message(nameId)}
    </Field>
    {choice('snmp_version', labels.snmpVersion, [{ value: 'v2c', label: 'SNMP v2c' }, { value: 'v3', label: 'SNMP v3' }])}
    {v3 ? <>
      <Field data-invalid={required.invalid(userId)}>
        <FieldLabel htmlFor={userId}>{labels.snmpUser}</FieldLabel>
        <Input id={userId} value={value('username')} onChange={(event) => onChange('username', event.target.value)} {...required.control(userId)} autoComplete="off" className="font-mono" />
        {required.message(userId)}
      </Field>
      <div className="grid gap-4 sm:grid-cols-2">
        {choice('auth_protocol', labels.authProtocol, [{ value: NONE, label: labels.noAuth }, ...snmpAuthProtocols.map((protocol) => ({ value: protocol, label: snmpProtocolName(protocol) }))])}
        {authOn ? secret('auth_password', labels.authPassword, labels.passwordRule) : null}
      </div>
      <div className="grid gap-4 sm:grid-cols-2">
        {choice('priv_protocol', labels.privProtocol, [{ value: NONE, label: labels.noPriv }, ...snmpPrivProtocols.map((protocol) => ({ value: protocol, label: snmpProtocolName(protocol) }))], !authOn)}
        {privOn ? secret('priv_password', labels.privPassword) : null}
      </div>
    </> : secret('community', labels.community)}
    <Field>
      <FieldLabel htmlFor={id('description')}>{labels.description}</FieldLabel>
      <Input id={id('description')} value={value('description')} onChange={(event) => onChange('description', event.target.value)} autoComplete="off" />
    </Field>
  </>
}

export function AssetEditDialog({ target, inventory, zh, onClose, onSave }: {
  target: EditTarget | null; inventory?: Inventory; zh: boolean; onClose: () => void; onSave: (path: string, body: Record<string, unknown>) => Promise<void>
}) {
  const labels = editorLabels(zh)
  return <Dialog open={target !== null} onOpenChange={(open) => { if (!open) onClose() }}>
    <DialogContent closeLabel={labels.close} className={cn('max-h-[90dvh] overflow-y-auto', target?.kind === 'profiles' ? 'sm:max-w-2xl' : 'sm:max-w-lg')}>
      {target ? <EditorForm key={`${target.kind}:${target.item.id}:${target.item.version}`} target={target} inventory={inventory} labels={labels} zh={zh} onSave={onSave} /> : null}
    </DialogContent>
  </Dialog>
}

export function DisableAssetDialog({ target, inventory, zh, onCancel, onConfirm }: {
  target: EditTarget | null; inventory?: Inventory; zh: boolean; onCancel: () => void; onConfirm: (target: EditTarget) => void
}) {
  const labels = editorLabels(zh)
  return <AlertDialog open={target !== null} onOpenChange={(open) => { if (!open) onCancel() }}>
    <AlertDialogContent>
      <AlertDialogHeader>
        <AlertDialogTitle>{target ? labels.disableTitle(target) : ''}</AlertDialogTitle>
        <AlertDialogDescription>{target ? labels.disableImpact(target, inventory) : ''}</AlertDialogDescription>
      </AlertDialogHeader>
      <AlertDialogFooter>
        <AlertDialogCancel>{labels.cancel}</AlertDialogCancel>
        <AlertDialogAction onClick={() => { if (target) onConfirm(target) }}>{labels.disable}</AlertDialogAction>
      </AlertDialogFooter>
    </AlertDialogContent>
  </AlertDialog>
}

const fieldNames: Record<string, [string, string]> = {
  name: ['name', '名称'], username: ['username', '用户名'], description: ['description', '描述'],
  encrypted_password: ['password', '密码'], encrypted_secret: ['privilege secret', '特权密码'],
  management_ip: ['management IP', '管理 IP'], credential_id: ['credential', '凭据'], command_profile_id: ['command profile', '命令模板'],
  switch_id: ['switch', '所属交换机'], cidr: ['CIDR', '网段'], vlan: ['VLAN', 'VLAN'], is_validated: ['validation', '验证状态'],
  vendor: ['vendor', '厂商'], model: ['model', '型号'], site: ['site', '站点'], protocol: ['protocol', '协议'], port: ['port', '端口'],
  group_id: ['group', '分组'], handles_release: ['release role', '负责解绑'],
  snmp_credential_id: ['SNMP credential', 'SNMP 凭据'], snmp_port: ['SNMP port', 'SNMP 端口'], snmp_version: ['SNMP version', 'SNMP 版本'],
  encrypted_community: ['community', '团体字'], auth_protocol: ['authentication', '认证方式'], priv_protocol: ['privacy', '加密方式'],
  encrypted_auth_password: ['authentication password', '认证密码'], encrypted_priv_password: ['privacy password', '加密密码'],
  interval_minutes: ['poll interval', '采集间隔'], cpu_threshold: ['CPU threshold', 'CPU 阈值'], memory_threshold: ['memory threshold', '内存阈值'],
  temperature_threshold: ['temperature threshold', '温度阈值'], consecutive_polls: ['polls in a row', '连续次数'], alert_on_restart: ['restart alerts', '重启告警'], kind: ['task type', '任务类型'], cron: ['schedule', '执行时间'], scope: ['scope', '范围'],
  pattern: ['pattern', '匹配规则'], mode: ['mode', '判定方式'], severity: ['severity', '严重级别'], switches: ['switches ungrouped', '移出分组的交换机'],
  ...Object.fromEntries(profileRuleKeys.map((key) => [key, [ruleText[key].label[0].toLowerCase(), ruleText[key].label[1]] as [string, string]])),
}

export const fieldName = (key: string, zh: boolean) => fieldNames[key]?.[zh ? 1 : 0] ?? key

// describeChanges summarizes an audit record without repeating any stored value; networks read
// from configuration are named, since they are no secret and say what releases now reach.
export function describeChanges(changes: Record<string, unknown> | undefined, zh: boolean): string {
  const parts = Object.entries(changes ?? {}).flatMap(([key, value]) => {
    if (key === 'is_active' || key === 'is_enabled') {
      const to = (value as { to?: unknown }).to
      return zh ? (to ? '启用' : '停用') : (to ? 'enabled' : 'disabled')
    }
    if (key === 'handles_release') {
      const to = (value as { to?: unknown }).to
      return zh ? (to ? '设为负责解绑' : '取消负责解绑') : (to ? 'now handles releases' : 'no longer handles releases')
    }
    if ((key === 'added' || key === 'removed') && Array.isArray(value)) {
      if (!value.length) return []
      const list = value.join(zh ? '、' : ', ')
      return key === 'added' ? (zh ? `新增网段 ${list}` : `networks added: ${list}`) : (zh ? `移除网段 ${list}` : `networks removed: ${list}`)
    }
    const name = fieldNames[key]?.[zh ? 1 : 0] ?? key
    if (value === 'replaced') return zh ? `${name}已更换` : `${name} replaced`
    if (value === 'cleared') return zh ? `${name}已删除` : `${name} removed`
    return name
  })
  return parts.length ? parts.join(zh ? '、' : ', ') : '—'
}

const actionNames: Record<string, [string, string]> = {
  'credential.create': ['Credential created', '新建凭据'], 'credential.update': ['Credential changed', '修改凭据'],
  'snmp_credential.create': ['SNMP credential created', '新建 SNMP 凭据'], 'snmp_credential.update': ['SNMP credential changed', '修改 SNMP 凭据'],
  'monitoring.update': ['Monitoring settings changed', '修改监控设置'],
  'profile.create': ['Profile created', '新建命令模板'], 'profile.update': ['Profile changed', '修改命令模板'],
  'switch.create': ['Switch created', '新建交换机'], 'switch.update': ['Switch changed', '修改交换机'],
  'switch.networks': ['Networks read from configuration', '从配置读取网段'],
  'network.create': ['Network created', '新建网段'], 'network.update': ['Network changed', '修改网段'],
  'switch.import': ['Switch imported', '导入交换机'], 'network.import': ['Network imported', '导入网段'],
  'group.create': ['Group created', '新建分组'], 'group.update': ['Group changed', '修改分组'], 'group.delete': ['Group deleted', '删除分组'],
  'schedule.create': ['Schedule created', '新建计划任务'], 'schedule.update': ['Schedule changed', '修改计划任务'], 'schedule.run': ['Schedule run manually', '手动执行计划'],
  'rule.create': ['Compliance rule created', '新建合规规则'], 'rule.update': ['Compliance rule changed', '修改合规规则'],
  'task.run': ['Task started', '发起任务'], 'config.view': ['Configuration viewed', '查看配置'], 'config.diff': ['Configurations compared', '对比配置'],
  'alert.acknowledge': ['Alert acknowledged', '确认告警'],
}

export function actionName(action: string, zh: boolean): string {
  return actionNames[action]?.[zh ? 1 : 0] ?? action
}
