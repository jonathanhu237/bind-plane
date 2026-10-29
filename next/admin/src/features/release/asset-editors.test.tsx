import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'
import {
  AssetEditDialog, AssetRequestError, DisableAssetDialog, describeChanges, editRequest,
  type Credential, type Inventory, type Profile, type SwitchAsset,
} from './asset-editors'

const credential: Credential = { id: 'c1', name: 'core', username: 'netops', description: null, is_active: true, has_secret: true, version: 2 }
const profile: Profile = {
  id: 'p1', name: 'vendor-a', is_active: true, version: 1, command_templates: { single_arp_query: 'show arp $ip' }, parser_rules: {},
  login_prompt_patterns: {}, prompt_patterns: {}, pagination_rules: {}, success_patterns: [], error_patterns: [],
}
const edge: SwitchAsset = { id: 's1', name: 'edge-1', management_ip: '192.0.2.10', credential_id: 'c1', command_profile_id: 'p1', is_enabled: true, version: 3 }
const inventory: Inventory = { credentials: [credential], profiles: [profile], switches: [edge, { ...edge, id: 's2', name: 'edge-2', management_ip: '192.0.2.11' }], networks: [] }

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
    expect(onSave).toHaveBeenCalledWith('switches/s1', { version: 3, name: 'edge-1', management_ip: '192.0.2.12', credential_id: 'c1', command_profile_id: 'p1' })
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
