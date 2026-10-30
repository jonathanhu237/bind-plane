import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useState } from 'react'
import { describe, expect, it, vi } from 'vitest'
import { Field, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { requiredMessage, useRequiredFields } from './required-fields'

function Form({ zh, onSave }: { zh: boolean; onSave: () => void }) {
  const [name, setName] = useState('')
  const [cidr, setCIDR] = useState('')
  const [note, setNote] = useState('')
  const required = useRequiredFields(zh, [
    { id: 'name', label: zh ? '名称' : 'Name', value: name },
    { id: 'cidr', label: 'IPv4 CIDR', value: cidr },
  ])
  return <form noValidate onSubmit={(event) => { event.preventDefault(); if (required.check()) onSave() }}>
    <Field data-invalid={required.invalid('name')}>
      <FieldLabel htmlFor="name">{zh ? '名称' : 'Name'}</FieldLabel>
      <Input id="name" value={name} onChange={(event) => setName(event.target.value)} {...required.control('name')} />
      {required.message('name')}
    </Field>
    <Field data-invalid={required.invalid('cidr')}>
      <FieldLabel htmlFor="cidr">IPv4 CIDR</FieldLabel>
      <Input id="cidr" value={cidr} onChange={(event) => setCIDR(event.target.value)} {...required.control('cidr', 'cidr-hint')} />
      <p id="cidr-hint">10.0.0.0/24</p>
      {required.message('cidr')}
    </Field>
    <Field>
      <FieldLabel htmlFor="note">{zh ? '备注' : 'Note'}</FieldLabel>
      <Input id="note" value={note} onChange={(event) => setNote(event.target.value)} />
    </Field>
    <button type="submit">Save</button>
  </form>
}

describe('required fields', () => {
  it('names each empty field in the interface language instead of saving', async () => {
    const onSave = vi.fn()
    const user = userEvent.setup()
    render(<Form zh onSave={onSave} />)
    await user.type(screen.getByLabelText('名称'), '   ')
    await user.click(screen.getByRole('button', { name: 'Save' }))
    expect(onSave).not.toHaveBeenCalled()
    expect(screen.getByText('请输入名称。')).toBeInTheDocument()
    expect(screen.getByText('请输入 IPv4 CIDR。')).toBeInTheDocument()
    expect(screen.getAllByRole('alert')).toHaveLength(2)
    expect(screen.getByLabelText('名称')).toHaveFocus()
    expect(screen.getByLabelText('名称')).toHaveAttribute('aria-invalid', 'true')
    expect(screen.getByLabelText('IPv4 CIDR')).toHaveAttribute('aria-describedby', 'cidr-hint cidr-required')
    expect(screen.getByLabelText('备注')).not.toHaveAttribute('aria-invalid')
  })

  it('clears a message as its field is filled, then saves', async () => {
    const onSave = vi.fn()
    const user = userEvent.setup()
    render(<Form zh onSave={onSave} />)
    await user.click(screen.getByRole('button', { name: 'Save' }))
    await user.type(screen.getByLabelText('名称'), 'core')
    expect(screen.queryByText('请输入名称。')).not.toBeInTheDocument()
    expect(screen.getByLabelText('名称')).not.toHaveAttribute('aria-invalid')
    expect(screen.getByLabelText('IPv4 CIDR')).toHaveAttribute('aria-describedby', 'cidr-hint cidr-required')
    await user.type(screen.getByLabelText('IPv4 CIDR'), '10.0.0.0/24')
    await user.click(screen.getByRole('button', { name: 'Save' }))
    expect(onSave).toHaveBeenCalledOnce()
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
  })

  it('asks to choose for selects, and says what is required in English', () => {
    expect(requiredMessage({ label: '凭据', choose: true }, true)).toBe('请选择凭据。')
    expect(requiredMessage({ label: '描述（可选）' }, true)).toBe('请输入描述。')
    expect(requiredMessage({ label: 'IPv4 CIDR' }, true)).toBe('请输入 IPv4 CIDR。')
    expect(requiredMessage({ label: 'Management IPv4' }, false)).toBe('Management IPv4 is required.')
  })
})
