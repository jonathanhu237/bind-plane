import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'
import { Label } from '@/components/ui/label'
import { useRequiredFields } from './required-fields'

function Form({ zh, onSave }: { zh: boolean; onSave: () => void }) {
  const required = useRequiredFields(zh)
  return <form noValidate onChange={required.onChange} onSubmit={(event) => { event.preventDefault(); if (required.check(event.currentTarget)) onSave() }}>
    <Label htmlFor="name">名称</Label><input id="name" required {...required.invalid('name')} />{required.message('name')}
    <Label htmlFor="cidr">IPv4 CIDR</Label><input id="cidr" required {...required.invalid('cidr', 'cidr-hint')} />{required.message('cidr')}
    <p id="cidr-hint">10.0.0.0/24</p>
    <Label htmlFor="group">分组（可选）</Label><select id="group" required {...required.invalid('group')}><option value="">—</option><option value="g1">Core</option></select>{required.message('group')}
    <Label htmlFor="note">备注</Label><input id="note" />
    <button type="submit">Save</button>
  </form>
}

describe('required fields', () => {
  it('names each empty field in the interface language instead of submitting', async () => {
    const onSave = vi.fn()
    const user = userEvent.setup()
    render(<Form zh onSave={onSave} />)
    await user.type(screen.getByLabelText('名称'), '   ')
    await user.click(screen.getByRole('button', { name: 'Save' }))
    expect(onSave).not.toHaveBeenCalled()
    expect(screen.getByText('请输入名称。')).toBeInTheDocument()
    expect(screen.getByText('请输入 IPv4 CIDR。')).toBeInTheDocument()
    expect(screen.getByText('请选择分组。')).toBeInTheDocument()
    expect(screen.getAllByRole('alert')).toHaveLength(3)
    expect(screen.getByLabelText('名称')).toHaveFocus()
    expect(screen.getByLabelText('名称')).toHaveAttribute('aria-invalid', 'true')
    expect(screen.getByLabelText('IPv4 CIDR')).toHaveAttribute('aria-describedby', 'cidr-hint cidr-required')
    expect(screen.getByLabelText('备注')).not.toHaveAttribute('aria-invalid')
  })

  it('clears a message as its field is edited, then submits once everything is filled', async () => {
    const onSave = vi.fn()
    const user = userEvent.setup()
    render(<Form zh onSave={onSave} />)
    await user.click(screen.getByRole('button', { name: 'Save' }))
    await user.type(screen.getByLabelText('名称'), 'core')
    expect(screen.queryByText('请输入名称。')).not.toBeInTheDocument()
    expect(screen.getByLabelText('名称')).not.toHaveAttribute('aria-invalid')
    expect(screen.getByText('请输入 IPv4 CIDR。')).toBeInTheDocument()
    await user.type(screen.getByLabelText('IPv4 CIDR'), '10.0.0.0/24')
    await user.selectOptions(screen.getByLabelText('分组（可选）'), 'g1')
    await user.click(screen.getByRole('button', { name: 'Save' }))
    expect(onSave).toHaveBeenCalledOnce()
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
  })

  it('uses plain English wording in the English interface', async () => {
    const user = userEvent.setup()
    render(<Form zh={false} onSave={vi.fn()} />)
    await user.click(screen.getByRole('button', { name: 'Save' }))
    expect(screen.getAllByText('This field is required.')).toHaveLength(2)
    expect(screen.getByText('Choose an option.')).toBeInTheDocument()
  })
})
