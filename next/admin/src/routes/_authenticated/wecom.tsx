import { createFileRoute, useLoaderData } from '@tanstack/react-router'
import { useQuery } from '@tanstack/react-query'
import { CircleAlert, Link2, Unlink2 } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { Field, FieldDescription, FieldGroup, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Select, SelectContent, SelectGroup, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { opsRequest, type WecomPage, type WecomState } from '@/features/ops/api'
import { AdminOnly, Empty, ErrorNote, PageHeader, Section, ToneBadge, Time, isAdmin, pick, useZh } from '@/features/ops/ui'
import { errorText, serverText } from '@/features/release/messages'
import { notifyRequestError, notifySuccess } from '@/shared/feedback'

export const Route = createFileRoute('/_authenticated/wecom')({ component: WecomPage })

const DEFAULT_URL = 'wss://openws.work.weixin.qq.com'

function StateBadge({ state, zh }: { state: WecomState; zh: boolean }) {
  const shown = {
    connected: ['good', pick(zh, '已连接', 'Connected')], connecting: ['muted', pick(zh, '正在连接', 'Connecting')],
    failed: ['bad', pick(zh, '连接失败', 'Not connected')], disabled: ['muted', pick(zh, '未启用', 'Off')],
  } as const
  return <ToneBadge kind={shown[state][0]}>{shown[state][1]}</ToneBadge>
}

function SettingsForm({ data, zh, onSaved }: { data: WecomPage['settings']; zh: boolean; onSaved: () => void }) {
  const [form, setForm] = useState({ is_enabled: data.is_enabled, bot_id: data.bot_id, secret: '', ws_url: data.ws_url || DEFAULT_URL })
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  async function submit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    setBusy(true); setError('')
    try {
      await opsRequest('wecom/settings', { version: data.version, is_enabled: form.is_enabled, bot_id: form.bot_id.trim(), secret: form.secret, ws_url: form.ws_url.trim() }, 'PATCH')
      setForm({ ...form, secret: '' })
      onSaved()
    } catch (cause) { setError(errorText(cause, zh)) }
    finally { setBusy(false) }
  }
  return <form className="flex max-w-xl flex-col gap-5" noValidate onSubmit={(event) => void submit(event)}>
    <FieldGroup>
      <Field orientation="horizontal">
        <Checkbox id="wecom-enabled" checked={form.is_enabled} onCheckedChange={(checked) => setForm({ ...form, is_enabled: checked === true })} />
        <FieldLabel htmlFor="wecom-enabled" className="font-normal">{pick(zh, '启用企业微信机器人', 'Turn the WeCom bot on')}</FieldLabel>
      </Field>
      <Field>
        <FieldLabel htmlFor="wecom-bot">{pick(zh, '机器人 ID', 'Bot ID')}</FieldLabel>
        <Input id="wecom-bot" className="font-mono" autoComplete="off" value={form.bot_id} onChange={(event) => setForm({ ...form, bot_id: event.target.value })} />
      </Field>
      <Field>
        <FieldLabel htmlFor="wecom-secret">Secret</FieldLabel>
        <Input id="wecom-secret" type="password" autoComplete="new-password" value={form.secret} placeholder={data.has_secret ? pick(zh, '已保存；留空表示不修改', 'Saved; leave blank to keep it') : ''}
          onChange={(event) => setForm({ ...form, secret: event.target.value })} aria-describedby="wecom-secret-hint" />
        <FieldDescription id="wecom-secret-hint">{pick(zh, '加密保存，保存后不再显示。', 'Stored encrypted and never shown again.')}</FieldDescription>
      </Field>
      <Field>
        <FieldLabel htmlFor="wecom-url">{pick(zh, '长连接地址', 'Connection address')}</FieldLabel>
        <Input id="wecom-url" className="font-mono" value={form.ws_url} onChange={(event) => setForm({ ...form, ws_url: event.target.value })} aria-describedby="wecom-url-hint" />
        <FieldDescription id="wecom-url-hint">{pick(zh, `一般不用改：${DEFAULT_URL}。私有部署的企业微信填管理后台给出的地址。`, `Usually ${DEFAULT_URL}. A privately deployed WeCom shows its own address in its admin console.`)}</FieldDescription>
      </Field>
    </FieldGroup>
    {error ? <Alert variant="destructive"><CircleAlert aria-hidden="true" /><AlertDescription>{error}</AlertDescription></Alert> : null}
    <div><Button type="submit" disabled={busy}>{busy ? pick(zh, '正在保存…', 'Saving…') : pick(zh, '保存', 'Save')}</Button></div>
  </form>
}

function BindForm({ data, zh, onBound }: { data: WecomPage; zh: boolean; onBound: () => void }) {
  const { t } = useTranslation()
  const bound = new Set(data.accounts.map((account) => account.user_id))
  const free = data.users.filter((user) => !bound.has(user.id))
  const [userID, setUserID] = useState('')
  const [account, setAccount] = useState('')
  const [busy, setBusy] = useState(false)
  async function submit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!userID || !account.trim()) return
    setBusy(true)
    try {
      await opsRequest(`wecom/accounts/${userID}`, { wecom_userid: account.trim() }, 'PUT')
      setUserID(''); setAccount('')
      notifySuccess(pick(zh, '已绑定。', 'Bound.'))
      onBound()
    } catch (cause) { notifyRequestError(cause, t, { description: errorText(cause, zh) }) }
    finally { setBusy(false) }
  }
  return <form className="flex flex-wrap items-end gap-2" onSubmit={(event) => void submit(event)}>
    <Field className="w-64">
      <FieldLabel htmlFor="bind-user">{pick(zh, 'Bind Plane 用户', 'Bind Plane user')}</FieldLabel>
      <Select value={userID} onValueChange={setUserID}>
        <SelectTrigger id="bind-user"><SelectValue placeholder={pick(zh, '请选择', 'Choose…')} /></SelectTrigger>
        <SelectContent><SelectGroup>{free.map((user) => <SelectItem key={user.id} value={user.id}>{user.name} <span className="text-muted-foreground">{user.email}</span></SelectItem>)}</SelectGroup></SelectContent>
      </Select>
    </Field>
    <Field className="w-56">
      <FieldLabel htmlFor="bind-account">{pick(zh, '企微账号', 'WeCom account')}</FieldLabel>
      <Input id="bind-account" className="font-mono" autoComplete="off" value={account} onChange={(event) => setAccount(event.target.value)} />
    </Field>
    <Button type="submit" variant="outline" disabled={busy || !userID || !account.trim()}><Link2 aria-hidden="true" data-icon="inline-start" />{pick(zh, '绑定', 'Bind')}</Button>
  </form>
}

function WecomPage() {
  const { user } = useLoaderData({ from: '/_authenticated' })
  const zh = useZh()
  const { t } = useTranslation()
  const admin = isAdmin(user)
  const query = useQuery({ queryKey: ['ops-wecom'], queryFn: () => opsRequest<WecomPage>('wecom'), enabled: admin, refetchInterval: 5000 })
  const [busy, setBusy] = useState(false)
  if (!admin) return <AdminOnly zh={zh} />
  const data = query.data
  async function unbind(userID: string, name: string) {
    setBusy(true)
    try { await opsRequest(`wecom/accounts/${userID}`, undefined, 'DELETE'); notifySuccess(pick(zh, `已解除 ${name} 的绑定。`, `${name} unbound.`)); await query.refetch() }
    catch (cause) { notifyRequestError(cause, t, { description: errorText(cause, zh) }) }
    finally { setBusy(false) }
  }
  const s = data?.settings
  return <main className="mx-auto flex w-full max-w-6xl flex-col gap-8">
    <PageHeader title={pick(zh, '企业微信', 'WeCom')} />
    <p className="-mt-5 max-w-3xl text-sm text-muted-foreground">{pick(zh,
      '绑定过账号的成员在企业微信里给机器人发 IP 地址、选择原因，就能解绑，结果会发回同一个对话。Bind Plane 主动连接企业微信（长连接），服务器不需要公网地址。',
      'Bound members send the bot an IP address in WeCom and choose a reason to release it; the result comes back to the same chat. Bind Plane connects out to WeCom over a long connection, so the server needs no public address.')}</p>
    <ErrorNote error={query.error} zh={zh} />
    {data && s ? <>
      <Section title={pick(zh, '连接', 'Connection')} actions={<StateBadge state={s.state} zh={zh} />}>
        <div className="flex flex-col gap-6">
          {s.state === 'connected' ? <p className="text-sm text-muted-foreground">{pick(zh, '连接于', 'Connected')} <Time value={s.connected_at} zh={zh} /></p> : null}
          {s.state === 'failed' && s.last_error ? <Alert variant="destructive"><CircleAlert aria-hidden="true" /><AlertDescription>{serverText(s.last_error, zh)}{s.state_at ? <span className="ml-1 text-xs opacity-80">（<Time value={s.state_at} zh={zh} />）</span> : null}</AlertDescription></Alert> : null}
          <SettingsForm key={s.version} data={s} zh={zh} onSaved={() => { notifySuccess(pick(zh, '已保存，几秒内按新设置重新连接。', 'Saved; it reconnects with these settings within seconds.')); void query.refetch() }} />
          <div className="max-w-3xl rounded-xl border bg-muted/40 p-4 text-sm">
            <p className="font-medium">{pick(zh, '在企业微信里准备机器人', 'Setting the bot up in WeCom')}</p>
            <ol className="mt-2 list-decimal pl-5 text-muted-foreground">
              <li>{pick(zh, '在企业微信管理后台新建一个智能机器人，选择「API 模式」，连接方式选「长连接」。', 'In the WeCom admin console, create a bot in API mode with a long connection.')}</li>
              <li>{pick(zh, '把它的机器人 ID 和 Secret 填到上面，勾选启用并保存；状态变成「已连接」就好了。', 'Enter its bot ID and secret above, turn it on and save; the state becomes Connected.')}</li>
              <li>{pick(zh, '一个机器人同时只能有一处连接：沙箱和生产环境要各建一个机器人，否则会互相挤掉。', 'A bot keeps only one connection at a time: the sandbox and production each need their own, or they push each other off.')}</li>
            </ol>
          </div>
        </div>
      </Section>
      <Section title={pick(zh, '账号绑定', 'Accounts')}>
        <div className="flex flex-col gap-4">
          <p className="max-w-3xl text-sm text-muted-foreground">{pick(zh,
            '把企业微信成员对应到 Bind Plane 用户。企微账号是通讯录成员详情里的「账号」。成员能做什么取决于对应的用户：有操作员角色才能解绑，解绑记录写的也是这个用户。',
            'Pair WeCom members with Bind Plane users; the WeCom account is the member’s account in the corporate directory. The user’s roles decide what the member may do: releasing needs the Operator role, and releases are recorded as that user.')}</p>
          <BindForm data={data} zh={zh} onBound={() => void query.refetch()} />
          {data.accounts.length ? <div className="overflow-x-auto rounded-xl border [&_th]:whitespace-nowrap"><Table>
            <TableHeader><TableRow><TableHead>{pick(zh, 'Bind Plane 用户', 'Bind Plane user')}</TableHead><TableHead>{pick(zh, '企微账号', 'WeCom account')}</TableHead><TableHead>{pick(zh, '绑定于', 'Bound')}</TableHead><TableHead className="text-right">{pick(zh, '操作', 'Actions')}</TableHead></TableRow></TableHeader>
            <TableBody>{data.accounts.map((account) => <TableRow key={account.user_id}>
              <TableCell><span className="font-medium">{account.name}</span> <span className="text-xs text-muted-foreground">{account.email}</span>{account.disabled ? <span className="ml-2"><ToneBadge kind="muted">{pick(zh, '用户已停用', 'User disabled')}</ToneBadge></span> : null}</TableCell>
              <TableCell className="font-mono text-sm">{account.wecom_userid}</TableCell>
              <TableCell className="whitespace-nowrap"><Time value={account.created_at} zh={zh} /></TableCell>
              <TableCell className="text-right"><Button type="button" variant="ghost" size="sm" disabled={busy} onClick={() => void unbind(account.user_id, account.name)}><Unlink2 aria-hidden="true" data-icon="inline-start" />{pick(zh, '解除', 'Unbind')}</Button></TableCell>
            </TableRow>)}</TableBody>
          </Table></div> : <Empty>{pick(zh, '还没有绑定任何成员。没绑定的成员给机器人发消息，会收到「请联系管理员绑定」的提示，并附上他的企微账号。', 'No member is bound yet. An unbound member who messages the bot is told to ask an admin, with their WeCom account.')}</Empty>}
        </div>
      </Section>
      <Section title={pick(zh, '在企业微信里怎么用', 'Using it in WeCom')}>
        <ul className="max-w-3xl list-disc pl-5 text-sm text-muted-foreground">
          <li>{pick(zh, '单聊机器人，或在群里 @ 它，发送「解绑 10.20.30.40」：它回一张卡片，写着这个地址归哪台交换机；点一个原因就开始解绑。', 'Message the bot, or @ it in a group, with “解绑 10.20.30.40”: it answers with the switch the address goes to; choosing a reason starts the release.')}</li>
          <li>{pick(zh, '发送「查询 10.20.30.40」只看它归哪台交换机和最近一次解绑，不会解绑。', '“查询 10.20.30.40” only shows the switch and the latest release.')}</li>
          <li>{pick(zh, '解绑完成后，结果发回同一个对话；失败、超时或结果未知时附「重新解绑」按钮。只有发起人能点自己的卡片。', 'The result comes back to the same chat, with Release again when it failed, timed out or is unknown. Only the member who asked can use their card.')}</li>
          <li>{pick(zh, '规则和解绑控制台一样：一个地址同时只能有一个解绑在进行；解绑前先查询，不是静态绑定就不发送任何命令。', 'The release console’s rules apply: one release per address at a time, and nothing is sent unless the binding is static.')}</li>
        </ul>
      </Section>
    </> : null}
  </main>
}
