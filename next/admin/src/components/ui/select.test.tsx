import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from './select'

function ExampleSelect() {
  return (
    <Select defaultValue="one">
      <SelectTrigger aria-label="Example filter">
        <SelectValue />
      </SelectTrigger>
      <SelectContent>
        <SelectItem value="one">One</SelectItem>
        <SelectItem value="two">Two</SelectItem>
      </SelectContent>
    </Select>
  )
}

describe('Select scroll behavior', () => {
  beforeEach(() => {
    HTMLElement.prototype.hasPointerCapture ??= () => false
    HTMLElement.prototype.setPointerCapture ??= () => undefined
    HTMLElement.prototype.releasePointerCapture ??= () => undefined
    HTMLElement.prototype.scrollIntoView ??= () => undefined
  })

  afterEach(() => {
    document.body.removeAttribute('data-select-scroll-lock')
    document.body.removeAttribute('data-scroll-locked')
  })

  it('marks standalone selects so the page scrollbar remains stable while open', async () => {
    const user = userEvent.setup()
    render(<ExampleSelect />)

    await user.click(screen.getByRole('combobox', { name: 'Example filter' }))
    await waitFor(() => expect(document.body).toHaveAttribute('data-select-scroll-lock'))

    await user.click(screen.getByRole('option', { name: 'Two' }))
    await waitFor(() => expect(document.body).not.toHaveAttribute('data-select-scroll-lock'))
  })

  it('leaves an existing dialog scroll lock untouched', async () => {
    document.body.setAttribute('data-scroll-locked', '1')
    const user = userEvent.setup()
    render(<ExampleSelect />)

    await user.click(screen.getByRole('combobox', { name: 'Example filter' }))
    expect(document.body).not.toHaveAttribute('data-select-scroll-lock')
  })
})
