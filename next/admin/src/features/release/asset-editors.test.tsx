import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'
import {
  AssetEditDialog, AssetRequestError, DisableAssetDialog, describeChanges, editRequest, portFollowing, profileFollowing,
  type Credential, type Inventory, type Profile, type SwitchAsset,
} from './asset-editors'

const credential: Credential = { id: 'c1', name: 'core', username: 'netops', description: null, is_active: true, has_secret: true, version: 2 }
const profile: Profile = {
  id: 'p1', name: 'vendor-a', vendor: 'huawei', is_active: true, version: 1, command_templates: { single_arp_query: 'show arp $ip' }, parser_rules: {},
  login_prompt_patterns: {}, prompt_patterns: {}, pagination_rules: {}, success_patterns: [], error_patterns: [],
}
const edge: SwitchAsset = {
  id: 's1', name: 'edge-1', management_ip: '192.0.2.10', credential_id: 'c1', command_profile_id: 'p1', is_enabled: true, version: 3,
  vendor: 'huawei', model: 'S5720', site: null, description: null, group_id: 'g1', protocol: 'ssh', port: null,
  handles_release: true, networks_succeeded_at: null, networks_error: null,
}
const inventory: Inventory = {
  credentials: [credential], profiles: [profile], switches: [edge, { ...edge, id: 's2', name: 'edge-2', management_ip: '192.0.2.11' }], networks: [],
  groups: [{ id: 'g1', name: 'Core', description: null, version: 1 }],
}

describe('asset edit requests', () => {
  it('keeps stored secrets unless they are replaced or removed', () => {
    const form = { name: 'core', username: 'netops', description: '', password: '', secret: '', clear_secret: false }
    expect(editRequest({ kind: 'credentials', item: credential }, form)).toEqual({
      path: 'credentials/c1', body: { version: 2, name: 'core', username: 'netops', description: '' },
    })
    expect(editRequest({ kind: 'credentials', item: credential }, { ...form, password: ' new ', secret: 'ignored', clear_secret: true })).toEqual({
      path: 'credentials/c1', body: { version: 2, name: 'core', username: 'netops', description: '', password: ' new ', clear_secret: true },
    })
  })

  it('refuses profile rules of the wrong JSON shape', () => {
    const rules = { name: 'vendor-a', command_templates: '{}', parser_rules: '{}', login_prompt_patterns: '{}', prompt_patterns: '{}', pagination_rules: '{}', success_patterns: '[]', error_patterns: '[]' }
    expect(editRequest({ kind: 'profiles', item: profile }, { ...rules, success_patterns: '{}' })).toEqual({ invalidRule: 'success_patterns' })
    expect(editRequest({ kind: 'profiles', item: profile }, { ...rules, parser_rules: '{' })).toEqual({ invalidRule: 'parser_rules' })
    expect(editRequest({ kind: 'profiles', item: profile }, rules)).toMatchObject({ path: 'profiles/p1', body: { version: 1, success_patterns: [] } })
  })

  it('rejects a port that is not a number', () => {
    const form = { name: 'edge-1', management_ip: '192.0.2.10', credential_id: 'c1', command_profile_id: 'p1', vendor: 'huawei', model: '', site: '', description: '', group_id: '', protocol: 'ssh', port: '22a' }
    expect(editRequest({ kind: 'switches', item: edge }, form)).toEqual({ invalidRule: 'port' })
  })

  it('keeps a switch on its protocol default following the protocol', () => {
    const form = { name: 'edge-1', management_ip: '192.0.2.10', credential_id: 'c1', command_profile_id: 'p1', vendor: 'huawei', model: '', site: '', description: '', group_id: '', protocol: 'telnet', port: '23' }
    const port = (item: SwitchAsset, fields: Record<string, string>) => {
      const request = editRequest({ kind: 'switches', item }, { ...form, ...fields })
      return 'body' in request ? request.body.port : request
    }
    expect(port(edge, {})).toBe(0)
    expect(port(edge, { port: '' })).toBe(0)
    expect(port(edge, { port: '2323' })).toBe(2323)
    expect(port({ ...edge, port: 22 }, { protocol: 'ssh', port: '22' })).toBe(22)
  })

  it('keeps a switch on a command profile of its own vendor', () => {
    const h3c: Profile = { ...profile, id: 'p2', name: 'comware', vendor: 'h3c' }
    const old: Profile = { ...profile, id: 'p3', name: 'comware-old', vendor: 'h3c', is_active: false }
    const profiles = [profile, h3c, old]
    expect(profileFollowing({ vendor: 'huawei', command_profile_id: 'p1' }, 'vendor', 'h3c', profiles)).toEqual({ command_profile_id: 'p2' })
    expect(profileFollowing({ vendor: 'h3c', command_profile_id: 'p3' }, 'vendor', 'h3c', profiles)).toEqual({})
    expect(profileFollowing({ vendor: 'huawei', command_profile_id: 'p1' }, 'vendor', 'cisco', profiles)).toEqual({ command_profile_id: '' })
    expect(profileFollowing({ vendor: 'huawei', command_profile_id: 'p1' }, 'vendor', 'h3c', [profile, h3c, { ...h3c, id: 'p4' }])).toEqual({ command_profile_id: '' })
    expect(profileFollowing({ vendor: 'huawei', command_profile_id: 'p1' }, 'model', 'S5720', profiles)).toEqual({})
  })

  it('moves a default port along when the protocol changes', () => {
    expect(portFollowing({ protocol: 'ssh', port: '22' }, 'protocol', 'telnet')).toEqual({ port: '23' })
    expect(portFollowing({ protocol: 'ssh', port: '' }, 'protocol', 'telnet')).toEqual({ port: '23' })
    expect(portFollowing({ protocol: 'ssh', port: '2222' }, 'protocol', 'telnet')).toEqual({})
    expect(portFollowing({ protocol: 'ssh', port: '22' }, 'model', 'S5720')).toEqual({})
  })

  it('summarizes audit changes without repeating values', () => {
    expect(describeChanges({ management_ip: { from: '192.0.2.10', to: '192.0.2.12' }, encrypted_password: 'replaced', is_enabled: { from: true, to: false } }, false))
      .toBe('management IP, password replaced, disabled')
    expect(describeChanges({}, false)).toBe('—')
  })
})

describe('asset editors', () => {
  it('saves a switch edit with the version it was loaded at', async () => {
    const onSave = vi.fn().mockResolvedValue(undefined)
    const user = userEvent.setup()
    render(<AssetEditDialog target={{ kind: 'switches', item: edge }} inventory={inventory} zh={false} onClose={vi.fn()} onSave={onSave} />)
    const address = await screen.findByLabelText('Management IPv4')
    await user.clear(address)
    await user.type(address, '192.0.2.12 ')
    await user.click(screen.getByRole('button', { name: 'Save changes' }))
    expect(onSave).toHaveBeenCalledWith('switches/s1', {
      version: 3, name: 'edge-1', management_ip: '192.0.2.12', credential_id: 'c1', command_profile_id: 'p1',
      vendor: 'huawei', model: 'S5720', site: '', description: '', group_id: 'g1', protocol: 'ssh', port: 0, handles_release: true,
    })
  })

  it('keeps the editor open with an actionable message when the asset changed elsewhere', async () => {
    const onSave = vi.fn().mockRejectedValue(new AssetRequestError('asset changed since it was loaded; reload and try again', 409))
    const user = userEvent.setup()
    render(<AssetEditDialog target={{ kind: 'switches', item: edge }} inventory={inventory} zh={false} onClose={vi.fn()} onSave={onSave} />)
    await user.click(await screen.findByRole('button', { name: 'Save changes' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('Close the editor and open it again')
    expect(screen.getByRole('dialog')).toBeInTheDocument()
  })

  it('states how many switches a disabled credential affects before confirming', async () => {
    const onConfirm = vi.fn()
    const user = userEvent.setup()
    render(<DisableAssetDialog target={{ kind: 'credentials', item: credential }} inventory={inventory} zh={false} onCancel={vi.fn()} onConfirm={onConfirm} />)
    const dialog = await screen.findByRole('alertdialog')
    expect(dialog).toHaveTextContent('2 switches use this credential')
    await user.click(within(dialog).getByRole('button', { name: 'Disable' }))
    expect(onConfirm).toHaveBeenCalledWith({ kind: 'credentials', item: credential })
  })
})
