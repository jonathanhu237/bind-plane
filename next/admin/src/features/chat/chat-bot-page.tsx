import { useLoaderData } from '@tanstack/react-router'
import { useQuery } from '@tanstack/react-query'
import { CircleAlert, Link2, Unlink2 } from 'lucide-react'
import { Fragment, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { Field, FieldDescription, FieldGroup, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Select, SelectContent, SelectGroup, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { opsRequest, type ChatBotPage as PageData, type ChatBotState, type ChatPlatform } from '@/features/ops/api'
import { AdminOnly, Empty, ErrorNote, PageHeader, Section, ToneBadge, Time, isAdmin, pick, useZh } from '@/features/ops/ui'
import { errorText, serverText } from '@/features/release/messages'
import { notifyRequestError, notifySuccess } from '@/shared/feedback'

/** What differs between the platforms' pages: their names, fields and how a bot is set up. */
function wording(platform: ChatPlatform, zh: boolean) {
  if (platform === 'feishu') {
    const address = 'https://open.feishu.cn'
    return {
      title: pick(zh, '飞书', 'Feishu'), address,
      intro: pick(zh, '绑定过账号的成员在飞书里给机器人发命令，就能查询或解绑地址，结果会发回同一个对话。Bind Plane 主动连接飞书开放平台（长连接），服务器不需要公网地址。',
        'Bound members send the bot commands in Feishu to look addresses up or release them; the result comes back to the same chat. Bind Plane connects out to Feishu’s open platform over a long connection, so the server needs no public address.'),
      enable: pick(zh, '启用飞书机器人', 'Turn the Feishu bot on'),
      appID: 'App ID',
      addressLabel: pick(zh, '开放平台地址', 'Open platform address'),
      addressHint: pick(zh, `飞书填 ${address}；Lark 国际版填 https://open.larksuite.com。`, `${address} for Feishu; https://open.larksuite.com for Lark.`),
      setupTitle: pick(zh, '在飞书开放平台准备应用', 'Setting the app up on Feishu’s open platform'),
      setup: [
        pick(zh, '在飞书开放平台（open.feishu.cn）创建一个「企业自建应用」，在「添加应用能力」里加上「机器人」。', 'On Feishu’s open platform (open.feishu.cn), create a custom app and add the Bot capability.'),
        pick(zh, '把「凭证与基础信息」里的 App ID 和 App Secret 填到上面，勾选启用并保存；状态变成「已连接」。', 'Enter the App ID and App Secret from its credentials above, turn it on and save; the state becomes Connected.'),
        pick(zh, '「权限管理」里开通：获取与发送单聊、群组消息，以应用的身份发消息，接收群聊中 @ 机器人消息，读取用户发给机器人的单聊消息；要用「用户 ID」绑定成员，再开通「获取用户 user ID」。', 'Under permissions, grant reading and sending direct and group messages, sending as the app, receiving group messages that @ the bot and reading direct messages to the bot; to bind members by user ID, also grant reading user IDs.'),
        pick(zh, '「事件与回调」里，事件和回调的订阅方式都选「使用长连接接收」（要先在上面连接成功才能保存），再添加事件「接收消息」和回调「卡片回传交互」。', 'Under events and callbacks, choose to receive both over a long connection (it saves only once the bot is connected above), then add the Receive message event and the card action callback.'),
        pick(zh, '创建版本并发布，管理员审核通过后，成员就能在飞书里找到这个机器人。', 'Create a version and publish it; once an admin approves it, members can find the bot in Feishu.'),
        pick(zh, '沙箱和生产环境各用一个应用：同一个应用的多处连接会随机分走消息。', 'The sandbox and production each need their own app: several connections of one app split its messages at random.'),
      ],
      account: pick(zh, '飞书 ID', 'Feishu ID'),
      accountsIntro: pick(zh, '把飞书成员对应到 Bind Plane 用户。飞书 ID 可以填管理后台成员详情里的「用户 ID」（应用要有「获取用户 user ID」权限），也可以填 open_id：没绑定的成员给机器人发消息时，机器人会告诉他。成员能做什么取决于对应的用户：有操作员角色才能解绑，解绑记录写的也是这个用户。',
        'Pair Feishu members with Bind Plane users, by the user ID shown in Feishu’s admin console (the app needs permission to read user IDs) or by open_id, which the bot tells a member who is not bound yet. The user’s roles decide what the member may do: releasing needs the Operator role, and releases are recorded as that user.'),
      emptyAccounts: pick(zh, '还没有绑定任何成员。没绑定的成员给机器人发消息，会收到「请联系管理员绑定」的提示，并附上他的飞书 ID。', 'No member is bound yet. An unbound member who messages the bot is told to ask an admin, with their Feishu ID.'),
      usageTitle: pick(zh, '在飞书里怎么用', 'Using it in Feishu'),
    }
  }
  const address = 'wss://openws.work.weixin.qq.com'
  return {
    title: pick(zh, '企业微信', 'WeCom'), address,
    intro: pick(zh, '绑定过账号的成员在企业微信里给机器人发命令，就能查询或解绑地址，结果会发回同一个对话。Bind Plane 主动连接企业微信（长连接），服务器不需要公网地址。',
      'Bound members send the bot commands in WeCom to look addresses up or release them; the result comes back to the same chat. Bind Plane connects out to WeCom over a long connection, so the server needs no public address.'),
    enable: pick(zh, '启用企业微信机器人', 'Turn the WeCom bot on'),
    appID: pick(zh, '机器人 ID', 'Bot ID'),
    addressLabel: pick(zh, '长连接地址', 'Connection address'),
    addressHint: pick(zh, `一般不用改：${address}。私有部署的企业微信填管理后台给出的地址。`, `Usually ${address}. A privately deployed WeCom shows its own address in its admin console.`),
    setupTitle: pick(zh, '在企业微信里准备机器人', 'Setting the bot up in WeCom'),
    setup: [
      pick(zh, '在企业微信管理后台新建一个智能机器人，选择「API 模式」，连接方式选「长连接」。', 'In the WeCom admin console, create a bot in API mode with a long connection.'),
      pick(zh, '把它的机器人 ID 和 Secret 填到上面，勾选启用并保存；状态变成「已连接」就好了。', 'Enter its bot ID and secret above, turn it on and save; the state becomes Connected.'),
      pick(zh, '一个机器人同时只能有一处连接：沙箱和生产环境要各建一个机器人，否则会互相挤掉。', 'A bot keeps only one connection at a time: the sandbox and production each need their own, or they push each other off.'),
    ],
    account: pick(zh, '企微账号', 'WeCom account'),
    accountsIntro: pick(zh, '把企业微信成员对应到 Bind Plane 用户。企微账号是通讯录成员详情里的「账号」。成员能做什么取决于对应的用户：有操作员角色才能解绑，解绑记录写的也是这个用户。',
      'Pair WeCom members with Bind Plane users; the WeCom account is the member’s account in the corporate directory. The user’s roles decide what the member may do: releasing needs the Operator role, and releases are recorded as that user.'),
    emptyAccounts: pick(zh, '还没有绑定任何成员。没绑定的成员给机器人发消息，会收到「请联系管理员绑定」的提示，并附上他的企微账号。', 'No member is bound yet. An unbound member who messages the bot is told to ask an admin, with their WeCom account.'),
    usageTitle: pick(zh, '在企业微信里怎么用', 'Using it in WeCom'),
  }
}

type Wording = ReturnType<typeof wording>

function StateBadge({ state, zh }: { state: ChatBotState; zh: boolean }) {
  const shown = {
    connected: ['good', pick(zh, '已连接', 'Connected')], connecting: ['muted', pick(zh, '正在连接', 'Connecting')],
    failed: ['bad', pick(zh, '连接失败', 'Not connected')], disabled: ['muted', pick(zh, '未启用', 'Off')],
  } as const
  return <ToneBadge kind={shown[state][0]}>{shown[state][1]}</ToneBadge>
}

function SettingsForm({ platform, words, data, zh, onSaved }: { platform: ChatPlatform; words: Wording; data: PageData['settings']; zh: boolean; onSaved: () => void }) {
  const [form, setForm] = useState({ is_enabled: data.is_enabled, app_id: data.app_id, secret: '', address: data.address || words.address })
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  async function submit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    setBusy(true); setError('')
    try {
      await opsRequest(`chat/${platform}/settings`, { version: data.version, is_enabled: form.is_enabled, app_id: form.app_id.trim(), secret: form.secret, address: form.address.trim() }, 'PATCH')
      setForm({ ...form, secret: '' })
      onSaved()
    } catch (cause) { setError(errorText(cause, zh)) }
    finally { setBusy(false) }
  }
  return <form className="flex max-w-xl flex-col gap-5" noValidate onSubmit={(event) => void submit(event)}>
    <FieldGroup>
      <Field orientation="horizontal">
        <Checkbox id="bot-enabled" checked={form.is_enabled} onCheckedChange={(checked) => setForm({ ...form, is_enabled: checked === true })} />
        <FieldLabel htmlFor="bot-enabled" className="font-normal">{words.enable}</FieldLabel>
      </Field>
      <Field>
        <FieldLabel htmlFor="bot-app">{words.appID}</FieldLabel>
        <Input id="bot-app" className="font-mono" autoComplete="off" value={form.app_id} onChange={(event) => setForm({ ...form, app_id: event.target.value })} />
      </Field>
      <Field>
        <FieldLabel htmlFor="bot-secret">{platform === 'feishu' ? 'App Secret' : 'Secret'}</FieldLabel>
        <Input id="bot-secret" type="password" autoComplete="new-password" value={form.secret} placeholder={data.has_secret ? pick(zh, '已保存；留空表示不修改', 'Saved; leave blank to keep it') : ''}
          onChange={(event) => setForm({ ...form, secret: event.target.value })} aria-describedby="bot-secret-hint" />
        <FieldDescription id="bot-secret-hint">{pick(zh, '加密保存，保存后不再显示。', 'Stored encrypted and never shown again.')}</FieldDescription>
      </Field>
      <Field>
        <FieldLabel htmlFor="bot-address">{words.addressLabel}</FieldLabel>
        <Input id="bot-address" className="font-mono" value={form.address} onChange={(event) => setForm({ ...form, address: event.target.value })} aria-describedby="bot-address-hint" />
        <FieldDescription id="bot-address-hint">{words.addressHint}</FieldDescription>
      </Field>
    </FieldGroup>
    {error ? <Alert variant="destructive"><CircleAlert aria-hidden="true" /><AlertDescription>{error}</AlertDescription></Alert> : null}
    <div><Button type="submit" disabled={busy}>{busy ? pick(zh, '正在保存…', 'Saving…') : pick(zh, '保存', 'Save')}</Button></div>
  </form>
}

function BindForm({ platform, words, data, zh, onBound }: { platform: ChatPlatform; words: Wording; data: PageData; zh: boolean; onBound: () => void }) {
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
      await opsRequest(`chat/${platform}/accounts/${userID}`, { external_id: account.trim() }, 'PUT')
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
      <FieldLabel htmlFor="bind-account">{words.account}</FieldLabel>
      <Input id="bind-account" className="font-mono" autoComplete="off" value={account} onChange={(event) => setAccount(event.target.value)} />
    </Field>
    <Button type="submit" variant="outline" disabled={busy || !userID || !account.trim()}><Link2 aria-hidden="true" data-icon="inline-start" />{pick(zh, '绑定', 'Bind')}</Button>
  </form>
}

/** A chat bot's settings, its connection, which member is which user, and the commands it takes. */
export function ChatBotPage({ platform }: { platform: ChatPlatform }) {
  const { user } = useLoaderData({ from: '/_authenticated' })
  const zh = useZh()
  const { t } = useTranslation()
  const admin = isAdmin(user)
  const query = useQuery({ queryKey: ['ops-chat-bot', platform], queryFn: () => opsRequest<PageData>(`chat/${platform}`), enabled: admin, refetchInterval: 5000 })
  const [busy, setBusy] = useState(false)
  if (!admin) return <AdminOnly zh={zh} />
  const words = wording(platform, zh)
  const data = query.data
  async function unbind(userID: string, name: string) {
    setBusy(true)
    try { await opsRequest(`chat/${platform}/accounts/${userID}`, undefined, 'DELETE'); notifySuccess(pick(zh, `已解除 ${name} 的绑定。`, `${name} unbound.`)); await query.refetch() }
    catch (cause) { notifyRequestError(cause, t, { description: errorText(cause, zh) }) }
    finally { setBusy(false) }
  }
  const s = data?.settings
  return <main className="mx-auto flex w-full max-w-6xl flex-col gap-8">
    <PageHeader title={words.title} />
    <p className="-mt-5 max-w-3xl text-sm text-muted-foreground">{words.intro}</p>
    <ErrorNote error={query.error} zh={zh} />
    {data && s ? <>
      <Section title={pick(zh, '连接', 'Connection')} actions={<StateBadge state={s.state} zh={zh} />}>
        <div className="flex flex-col gap-6">
          {s.state === 'connected' ? <p className="text-sm text-muted-foreground">{pick(zh, '连接于', 'Connected')} <Time value={s.connected_at} zh={zh} /></p> : null}
          {s.state === 'failed' && s.last_error ? <Alert variant="destructive"><CircleAlert aria-hidden="true" /><AlertDescription>{serverText(s.last_error, zh)}{s.state_at ? <span className="ml-1 text-xs opacity-80">（<Time value={s.state_at} zh={zh} />）</span> : null}</AlertDescription></Alert> : null}
          <SettingsForm key={s.version} platform={platform} words={words} data={s} zh={zh} onSaved={() => { notifySuccess(pick(zh, '已保存，几秒内按新设置重新连接。', 'Saved; it reconnects with these settings within seconds.')); void query.refetch() }} />
          <div className="max-w-3xl rounded-xl border bg-muted/40 p-4 text-sm">
            <p className="font-medium">{words.setupTitle}</p>
            <ol className="mt-2 list-decimal pl-5 text-muted-foreground">{words.setup.map((step) => <li key={step}>{step}</li>)}</ol>
          </div>
        </div>
      </Section>
      <Section title={pick(zh, '账号绑定', 'Accounts')}>
        <div className="flex flex-col gap-4">
          <p className="max-w-3xl text-sm text-muted-foreground">{words.accountsIntro}</p>
          <BindForm platform={platform} words={words} data={data} zh={zh} onBound={() => void query.refetch()} />
          {data.accounts.length ? <div className="overflow-x-auto rounded-xl border [&_th]:whitespace-nowrap"><Table>
            <TableHeader><TableRow><TableHead>{pick(zh, 'Bind Plane 用户', 'Bind Plane user')}</TableHead><TableHead>{words.account}</TableHead><TableHead>{pick(zh, '绑定于', 'Bound')}</TableHead><TableHead className="text-right">{pick(zh, '操作', 'Actions')}</TableHead></TableRow></TableHeader>
            <TableBody>{data.accounts.map((account) => <TableRow key={account.user_id}>
              <TableCell><span className="font-medium">{account.name}</span> <span className="text-xs text-muted-foreground">{account.email}</span>{account.disabled ? <span className="ml-2"><ToneBadge kind="muted">{pick(zh, '用户已停用', 'User disabled')}</ToneBadge></span> : null}</TableCell>
              <TableCell className="font-mono text-sm">{account.external_id}</TableCell>
              <TableCell className="whitespace-nowrap"><Time value={account.created_at} zh={zh} /></TableCell>
              <TableCell className="text-right"><Button type="button" variant="ghost" size="sm" disabled={busy} onClick={() => void unbind(account.user_id, account.name)}><Unlink2 aria-hidden="true" data-icon="inline-start" />{pick(zh, '解除', 'Unbind')}</Button></TableCell>
            </TableRow>)}</TableBody>
          </Table></div> : <Empty>{words.emptyAccounts}</Empty>}
        </div>
      </Section>
      <Section title={words.usageTitle}>
        <div className="flex max-w-3xl flex-col gap-3 text-sm text-muted-foreground">
          <p>{pick(zh, '单聊机器人，或在群里 @ 它，命令写在最前面：', 'Message the bot, or @ it in a group, starting with the command:')}</p>
          <dl className="grid gap-x-4 gap-y-2 sm:grid-cols-[auto_minmax(0,1fr)]">
            {[
              ['查询 10.20.30.40', pick(zh, '向交换机查这个地址的 MAC 和绑定类型，不改动任何东西；是静态绑定时可以接着点「解绑这个地址」。', 'Asks the switch for the address’s MAC and entry type and changes nothing; a static binding can then be released from the result.')],
              ['解绑 10.20.30.40', pick(zh, '回一张卡片，写着这个地址归哪台交换机；点一个原因就开始解绑。', 'Answers with a card naming the switch; choosing a reason starts the release.')],
              ['解绑 10.20.30.40 临时测试', pick(zh, '带上原因，一条消息直接开始解绑。原因只能是：临时测试、用户反馈、IP/MAC 变更、修正错误绑定、安全处置、其他。', 'With a reason, the release starts at once. The reason is one of 临时测试, 用户反馈, IP/MAC 变更, 修正错误绑定, 安全处置, 其他.')],
              ['帮助', pick(zh, '列出这些命令。', 'Lists these commands.')],
            ].map(([command, meaning]) => <Fragment key={command}><dt><code className="rounded bg-muted px-1.5 py-0.5 font-mono text-xs text-foreground">{command}</code></dt><dd>{meaning}</dd></Fragment>)}
          </dl>
          <ul className="list-disc pl-5">
            <li>{pick(zh, '英文命令 query、release、help 也可以。其他写法（只发一个地址、随口一句话、不在列表里的原因）只会收到这份说明，不会触发任何操作。', 'The English query, release and help work too. Anything else (an address on its own, other text, a reason not on the list) only gets these instructions and starts nothing.')}</li>
            <li>{pick(zh, '解绑完成后，结果发回同一个对话；失败、超时或结果未知时附「重新解绑」按钮。只有发起人能点自己的卡片。', 'The result comes back to the same chat, with Release again when it failed, timed out or is unknown. Only the member who asked can use their card.')}</li>
            <li>{pick(zh, '规则和解绑控制台一样：一个地址同时只能有一个解绑在进行；解绑前先查询，不是静态绑定就不发送任何命令。', 'The release console’s rules apply: one release per address at a time, and nothing is sent unless the binding is static.')}</li>
          </ul>
        </div>
      </Section>
    </> : null}
  </main>
}
