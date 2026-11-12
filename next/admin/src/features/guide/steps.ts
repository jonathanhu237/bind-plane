/**
 * The guided tour shown at a user's first sign-in. Raising GUIDE_VERSION shows it once more to
 * everyone; do that only when the tour changes enough to be worth another look.
 */
export const GUIDE_VERSION = 1

export type GuideSide = 'right' | 'bottom' | 'top'

export type GuideStep = {
  id: string
  /** The page the step is on; the tour goes there first. Sidebar steps stay where the user is. */
  route?: '/' | '/release'
  /** data-guide values to highlight, the first one on screen wins; none, or none on screen, centres the card. */
  targets: string[]
  /** Where the card sits beside the highlight, when there is room. */
  side?: GuideSide
  title: { zh: string; en: string }
  body: { zh: string; en: string }
}

const welcome: GuideStep = {
  id: 'welcome',
  targets: [],
  title: { zh: '欢迎使用 Bind Plane', en: 'Welcome to Bind Plane' },
  body: {
    zh: '这里只做一件事：删除一个 IPv4 地址在交换机上的静态绑定。花一分钟看看各部分在哪里，随时可以跳过。',
    en: 'It does one thing: delete the static IP-MAC binding of one IPv4 address on its switch. Take a minute to see where things are; you can skip at any time.',
  },
}

/** What an admin sets up once, in the order that makes releases possible. */
const setup: GuideStep[] = [
  {
    id: 'credentials',
    targets: ['nav-credentials', 'nav-assets'],
    side: 'right',
    title: { zh: '第一步：凭据', en: 'Step 1: credentials' },
    body: {
      zh: '先添加登录交换机用的用户名和密码（Telnet）。密码加密保存，保存后不再显示。',
      en: 'First add the user name and password that sign in to the switches over Telnet. Passwords are stored encrypted and never shown again.',
    },
  },
  {
    id: 'command-profiles',
    targets: ['nav-command-profiles', 'nav-assets'],
    side: 'right',
    title: { zh: '第二步：命令模板', en: 'Step 2: command profiles' },
    body: {
      zh: '已经内置 H3C、华为、锐捷等常见交换机的模板，一般直接用。设备的命令不一样时，复制一份再改。',
      en: 'Profiles for H3C, Huawei, Ruijie and other common switches are built in and usually work as they are. Copy one and edit it when a switch speaks differently.',
    },
  },
  {
    id: 'devices',
    targets: ['nav-devices', 'nav-ops'],
    side: 'right',
    title: { zh: '第三步：设备', en: 'Step 3: devices' },
    body: {
      zh: '在这里添加交换机或批量导入，选好凭据和命令模板。网关交换机要标记为「负责解绑」，系统会从它的配置里读出网段。',
      en: 'Add switches here or import them, choosing a credential and a command profile. Mark the gateways as handling releases; their networks are then read from their configuration.',
    },
  },
  {
    id: 'networks',
    targets: ['nav-networks', 'nav-assets'],
    side: 'right',
    title: { zh: '网段', en: 'Networks' },
    body: {
      zh: '网段从负责解绑的交换机读出，不用手填。输入一个地址，就能看到解绑时会找哪台交换机。',
      en: 'Networks are read from the release switches, never typed in. Enter an address to see which switch a release of it would go to.',
    },
  },
  {
    id: 'chat',
    targets: ['nav-wecom', 'nav-assets'],
    side: 'right',
    title: { zh: '企业微信和飞书', en: 'WeCom and Feishu' },
    body: {
      zh: '想在聊天里解绑，就在「企业微信」「飞书」里接入机器人，再把成员绑定到他们的 Bind Plane 账号。',
      en: 'To release from a chat, connect the bots under WeCom and Feishu, then bind members to their Bind Plane accounts.',
    },
  },
  {
    id: 'users',
    targets: ['nav-users', 'nav-access'],
    side: 'right',
    title: { zh: '用户', en: 'Users' },
    body: {
      zh: '在「用户」里直接创建账号或发邀请，给操作员（Operator）或管理员（Admin）角色。',
      en: 'Create accounts directly or send invitations under Users, with the Operator or Admin role.',
    },
  },
]

/** How a release goes, for everyone who can release. */
const daily: GuideStep[] = [
  {
    id: 'home',
    route: '/',
    targets: ['nav-home'],
    side: 'right',
    title: { zh: '首页', en: 'Home' },
    body: {
      zh: '每次登录先到这里。结果未知、还没有重新解绑的记录排在最前，下面是最近的解绑。这里只看情况，不发起操作。',
      en: 'You land here after signing in. Releases with an unknown outcome that nobody has released again come first, then recent releases. Nothing starts from here.',
    },
  },
  {
    id: 'new-release',
    route: '/',
    targets: ['home-new-release'],
    side: 'bottom',
    title: { zh: '发起解绑', en: 'New release' },
    body: { zh: '从这里打开解绑控制台。', en: 'Opens the release console.' },
  },
  {
    id: 'release-input',
    route: '/release',
    targets: ['release-input'],
    side: 'bottom',
    title: { zh: '地址和原因', en: 'Address and reason' },
    body: {
      zh: '输入一个 IPv4 地址，选一个原因。系统按网段自动找到负责的交换机，不用自己选。',
      en: 'Enter one IPv4 address and choose a reason. The switch is found from the address’s network; you never pick it.',
    },
  },
  {
    id: 'lookup',
    route: '/release',
    targets: ['release-lookup'],
    side: 'bottom',
    title: { zh: '查询', en: 'Query' },
    body: {
      zh: '只想看看？「查询」登录交换机，读出 MAC 和绑定类型，不做任何修改。查到静态绑定，可以直接从结果里解绑。',
      en: 'Just looking? Query signs in to the switch and reads the MAC and the entry type, changing nothing. A static binding it finds can be released from the result.',
    },
  },
  {
    id: 'release',
    route: '/release',
    targets: ['release-submit'],
    side: 'bottom',
    title: { zh: '解绑', en: 'Release' },
    body: {
      zh: '点「解绑」立刻开始，不再二次确认。系统先查一次，是静态绑定才删除，删完再查一次。只有动态记录或查无此 IP 时，什么都不发送。',
      en: 'Release starts at once, without asking again. The switch is checked first and the binding deleted only if it is static, then checked again. With only a dynamic entry, or no record of the address, nothing is sent.',
    },
  },
  {
    id: 'history',
    targets: ['nav-releases', 'nav-release'],
    side: 'right',
    title: { zh: '解绑记录', en: 'Release history' },
    body: {
      zh: '所有解绑和查询都在这里，可以筛选。失败、超时或结果未知的，打开后可以重新解绑。',
      en: 'Every release and lookup is here, with filters. Open a failed, timed-out or unknown one to release it again.',
    },
  },
]

const done: GuideStep = {
  id: 'done',
  targets: ['account-menu'],
  side: 'right',
  title: { zh: '就这些', en: 'That’s it' },
  body: {
    zh: '也可以在企业微信或飞书里发「解绑 <IP>」，需要管理员先绑定你的账号。想再看一遍，从这里的「使用指引」打开。',
    en: 'You can also send “解绑 <IP>” in WeCom or Feishu once an admin has bound your account. To see this again, open Guided tour from this menu.',
  },
}

/** The tour for one user: admins see the setup first, everyone who can release sees how it goes. */
export function guideSteps({ admin, release }: { admin: boolean; release: boolean }): GuideStep[] {
  return [welcome, ...(admin ? setup : []), ...(release ? daily : []), done]
}
