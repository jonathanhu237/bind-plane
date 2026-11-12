import { act, cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { GuidedTour, placeCard } from './guided-tour'
import { guideSteps, type GuideStep } from './steps'

const navigate = vi.fn()
let pathname = '/'
// Like the real router, one object for the whole session.
const router = { state: { location: { get pathname() { return pathname } } } }
vi.mock('@tanstack/react-router', () => ({
  useNavigate: () => navigate,
  useRouter: () => router,
}))

/** A page element the tour can find: jsdom lays nothing out, so give it a size. */
function place(id: string, box = { top: 100, left: 40, width: 120, height: 32 }) {
  const element = document.createElement('div')
  element.dataset.guide = id
  element.getBoundingClientRect = () => ({ ...box, x: box.left, y: box.top, bottom: box.top + box.height, right: box.left + box.width, toJSON: () => box })
  element.scrollIntoView = () => {}
  document.body.append(element)
  return element
}

const steps: GuideStep[] = [
  { id: 'one', targets: [], title: { zh: '欢迎', en: 'Welcome' }, body: { zh: '开始', en: 'Start' } },
  { id: 'two', targets: ['missing', 'nav-home'], side: 'right', title: { zh: '首页', en: 'Home' }, body: { zh: '首页说明', en: 'Home body' } },
  { id: 'three', route: '/release', targets: ['release-input'], title: { zh: '地址', en: 'Address' }, body: { zh: '地址说明', en: 'Address body' } },
]

describe('guideSteps', () => {
  it('shows admins the setup first and everyone who can release how it goes', () => {
    const admin = guideSteps({ admin: true, release: true }).map((step) => step.id)
    const operator = guideSteps({ admin: false, release: true }).map((step) => step.id)
    expect(admin[0]).toBe('welcome')
    expect(admin.indexOf('credentials')).toBeLessThan(admin.indexOf('devices'))
    expect(admin.indexOf('users')).toBeLessThan(admin.indexOf('home'))
    expect(operator).not.toContain('credentials')
    expect(operator).toContain('release')
    expect(operator.at(-1)).toBe('done')
    expect(guideSteps({ admin: false, release: false }).map((step) => step.id)).toEqual(['welcome', 'done'])
  })

  it('never says 释放', () => {
    for (const step of guideSteps({ admin: true, release: true })) {
      expect(step.title.zh + step.body.zh).not.toMatch(/释放/)
    }
  })
})

describe('placeCard', () => {
  const viewport = { width: 1200, height: 800 }
  const size = { width: 320, height: 180 }

  it('centres the card without a highlight', () => {
    expect(placeCard(null, size, undefined, viewport)).toEqual({ top: 310, left: 440 })
  })

  it('puts the card on the preferred side when it fits', () => {
    expect(placeCard({ top: 100, left: 20, width: 200, height: 30 }, size, 'right', viewport)).toEqual({ top: 100, left: 232 })
    expect(placeCard({ top: 100, left: 400, width: 200, height: 30 }, size, 'bottom', viewport)).toEqual({ top: 142, left: 400 })
  })

  it('moves to another side, and stays inside the window', () => {
    // No room on the right: below instead.
    expect(placeCard({ top: 100, left: 900, width: 200, height: 30 }, size, 'right', viewport)).toEqual({ top: 142, left: 864 })
    // No room below: above.
    expect(placeCard({ top: 700, left: 400, width: 200, height: 40 }, size, 'bottom', viewport)).toEqual({ top: 508, left: 400 })
    // Near the bottom on the right: pulled up to stay inside.
    expect(placeCard({ top: 760, left: 20, width: 200, height: 30 }, size, 'right', viewport)).toEqual({ top: 604, left: 232 })
  })
})

describe('GuidedTour', () => {
  let root: HTMLDivElement
  beforeEach(() => {
    navigate.mockReset()
    pathname = '/'
    root = document.createElement('div')
    root.id = 'root'
    document.body.append(root)
  })
  afterEach(() => {
    cleanup()
    document.body.innerHTML = ''
  })

  it('walks through the steps over the page, which cannot be used meanwhile', async () => {
    place('nav-home')
    place('release-input', { top: 200, left: 300, width: 200, height: 40 })
    place('release-input', { top: 200, left: 520, width: 200, height: 40 })
    const onClose = vi.fn()
    const { unmount } = render(<GuidedTour steps={steps} zh onClose={onClose} />)

    expect(await screen.findByRole('dialog', { name: '欢迎' })).toHaveAccessibleDescription('开始')
    expect(screen.getByText('1 / 3')).toBeInTheDocument()
    expect(screen.queryByTestId('guide-highlight')).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: '下一步' })).toHaveFocus()
    expect(root.inert).toBe(true)

    fireEvent.click(screen.getByRole('button', { name: '下一步' }))
    // The first target is not on the page, the second is.
    expect(await screen.findByRole('dialog', { name: '首页' })).toBeInTheDocument()
    expect(screen.getByTestId('guide-highlight')).toHaveStyle({ top: '94px', left: '34px', width: '132px', height: '44px' })
    expect(navigate).not.toHaveBeenCalled()

    fireEvent.keyDown(screen.getByRole('dialog'), { key: 'ArrowRight' })
    expect(await screen.findByRole('dialog', { name: '地址' })).toBeInTheDocument()
    expect(navigate).toHaveBeenCalledWith({ to: '/release' })
    // Both fields of one target are lit together.
    expect(screen.getByTestId('guide-highlight')).toHaveStyle({ top: '194px', left: '294px', width: '432px', height: '52px' })
    expect(screen.queryByRole('button', { name: '跳过' })).not.toBeInTheDocument()

    fireEvent.keyDown(screen.getByRole('dialog'), { key: 'ArrowLeft' })
    expect(await screen.findByRole('dialog', { name: '首页' })).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '下一步' }))
    fireEvent.click(await screen.findByRole('button', { name: '完成' }))
    expect(onClose).toHaveBeenCalledTimes(1)

    unmount()
    expect(root.inert).toBe(false)
  })

  it('ends on Esc or Skip, and keeps focus on its buttons', async () => {
    const onClose = vi.fn()
    render(<GuidedTour steps={steps} zh={false} onClose={onClose} />)
    const dialog = await screen.findByRole('dialog', { name: 'Welcome' })
    const next = screen.getByRole('button', { name: 'Next' })
    const skip = screen.getByRole('button', { name: 'Skip' })
    expect(next).toHaveFocus()
    fireEvent.keyDown(dialog, { key: 'Tab' })
    expect(skip).toHaveFocus()
    fireEvent.keyDown(dialog, { key: 'Tab', shiftKey: true })
    expect(next).toHaveFocus()
    // Keys count even when focus has left the card.
    fireEvent.keyDown(document.body, { key: 'Escape' })
    fireEvent.click(skip)
    expect(onClose).toHaveBeenCalledTimes(2)
  })

  it('centres a step whose element never appears', async () => {
    vi.useFakeTimers()
    try {
      render(<GuidedTour steps={[steps[1], steps[0]]} zh onClose={() => {}} />)
      expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
      await act(async () => { await vi.advanceTimersByTimeAsync(2100) })
      expect(screen.getByRole('dialog', { name: '首页' })).toBeInTheDocument()
      expect(screen.queryByTestId('guide-highlight')).not.toBeInTheDocument()
    } finally {
      vi.useRealTimers()
    }
  })
})
