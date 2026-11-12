import { useNavigate, useRouter } from '@tanstack/react-router'
import { useEffect, useId, useLayoutEffect, useRef, useState, type CSSProperties, type KeyboardEvent } from 'react'
import { createPortal } from 'react-dom'
import { Button } from '@/components/ui/button'
import { Card, CardDescription, CardFooter, CardHeader, CardTitle } from '@/components/ui/card'
import { pick } from '@/features/ops/ui'
import type { GuideSide, GuideStep } from './steps'

type Box = { top: number; left: number; width: number; height: number }
type Size = { width: number; height: number }

/** Room around the highlighted element, the gap to the card, and the card's distance from the window's edge. */
const PAD = 6
const GAP = 12
const MARGIN = 16
/** How long a step waits for its element after going to its page (50 ms × 40). */
const LOOKS = 40

/**
 * The elements of the first of the step's targets that is on screen (sidebar items are absent on
 * phones or in a closed group). Several elements may share one target and are lit together.
 */
function findTargets(ids: string[]): HTMLElement[] {
  for (const id of ids) {
    const elements = [...document.querySelectorAll<HTMLElement>(`[data-guide="${id}"]`)].filter((element) => {
      const box = element.getBoundingClientRect()
      return box.width > 0 && box.height > 0
    })
    if (elements.length) return elements
  }
  return []
}

function measure(elements: HTMLElement[]): Box | null {
  const boxes = elements.filter((element) => element.isConnected).map((element) => element.getBoundingClientRect())
  if (!boxes.length) return null
  const top = Math.min(...boxes.map((box) => box.top))
  const left = Math.min(...boxes.map((box) => box.left))
  const bottom = Math.max(...boxes.map((box) => box.bottom))
  const right = Math.max(...boxes.map((box) => box.right))
  return { top: top - PAD, left: left - PAD, width: right - left + PAD * 2, height: bottom - top + PAD * 2 }
}

/** Puts the card beside the highlight on the preferred side when it fits, else on another, always inside the window. */
export function placeCard(box: Box | null, size: Size, preferred: GuideSide | undefined, viewport: Size): CSSProperties {
  const x = (left: number) => Math.min(Math.max(left, MARGIN), Math.max(MARGIN, viewport.width - size.width - MARGIN))
  const y = (top: number) => Math.min(Math.max(top, MARGIN), Math.max(MARGIN, viewport.height - size.height - MARGIN))
  if (!box) return { top: y((viewport.height - size.height) / 2), left: x((viewport.width - size.width) / 2) }
  const order: GuideSide[] = preferred === 'right' ? ['right', 'bottom', 'top'] : preferred === 'top' ? ['top', 'bottom', 'right'] : ['bottom', 'top', 'right']
  for (const side of order) {
    if (side === 'right' && box.left + box.width + GAP + size.width + MARGIN <= viewport.width) return { top: y(box.top), left: box.left + box.width + GAP }
    if (side === 'bottom' && box.top + box.height + GAP + size.height + MARGIN <= viewport.height) return { top: box.top + box.height + GAP, left: x(box.left) }
    if (side === 'top' && box.top - GAP - size.height >= MARGIN) return { top: box.top - GAP - size.height, left: x(box.left) }
  }
  return { top: y(box.top + box.height + GAP), left: x(box.left) }
}

/**
 * Walks through the steps over the real pages: the page dims, the step's element stays lit, and a
 * card beside it says what it is. The page underneath cannot be used meanwhile. Esc skips, the
 * arrow keys move between steps; skipping and finishing both end it.
 */
export function GuidedTour({ steps, zh, onClose }: { steps: GuideStep[]; zh: boolean; onClose: () => void }) {
  const [index, setIndex] = useState(0)
  const [found, setFound] = useState<{ index: number; box: Box | null } | null>(null)
  const [size, setSize] = useState<Size>({ width: 320, height: 200 })
  const [viewport, setViewport] = useState<Size>({ width: window.innerWidth, height: window.innerHeight })
  const target = useRef<HTMLElement[]>([])
  const card = useRef<HTMLDivElement>(null)
  const primary = useRef<HTMLButtonElement>(null)
  const navigate = useNavigate()
  const router = useRouter()
  const titleID = useId()
  const bodyID = useId()
  const step = steps[index]
  const last = index === steps.length - 1
  const ready = found?.index === index

  // Keep the page out of reach while the tour runs: no clicks, no focus, hidden from screen readers.
  useEffect(() => {
    const root = document.getElementById('root')
    if (!root) return
    root.inert = true
    return () => { root.inert = false }
  }, [])

  // Go to the step's page, then wait briefly for its element; without one the card sits in the middle.
  useEffect(() => {
    target.current = []
    // The router's path leaves out the base path; staying put keeps a job the console shows.
    if (step.route && router.state.location.pathname !== step.route) void navigate({ to: step.route })
    let looks = 0
    let timer = 0
    const look = () => {
      const elements = findTargets(step.targets)
      if (elements.length || !step.targets.length || ++looks >= LOOKS) {
        elements[0]?.scrollIntoView({ block: 'nearest', inline: 'nearest' })
        target.current = elements
        setFound({ index, box: measure(elements) })
      } else timer = window.setTimeout(look, 50)
    }
    look()
    return () => window.clearTimeout(timer)
  }, [index, step, navigate, router])

  // Follow the element when the window, the page or the element itself moves.
  useEffect(() => {
    if (!ready) return
    let frame = 0
    const update = () => {
      cancelAnimationFrame(frame)
      frame = requestAnimationFrame(() => {
        setViewport({ width: window.innerWidth, height: window.innerHeight })
        setFound({ index, box: measure(target.current) })
      })
    }
    window.addEventListener('resize', update)
    window.addEventListener('scroll', update, true)
    const observer = new ResizeObserver(update)
    for (const element of target.current) observer.observe(element)
    return () => {
      cancelAnimationFrame(frame)
      window.removeEventListener('resize', update)
      window.removeEventListener('scroll', update, true)
      observer.disconnect()
    }
  }, [ready, index])

  // The card's size follows its text and the window's width; measure it before it is painted.
  useLayoutEffect(() => {
    if (!card.current) return
    const next = { width: card.current.offsetWidth, height: card.current.offsetHeight }
    setSize((current) => (current.width === next.width && current.height === next.height ? current : next))
  }, [ready, index, zh, viewport.width])

  useEffect(() => {
    if (!ready) return
    primary.current?.focus()
    // The menu that opened the tour may still be closing and take focus with it; take it back.
    const timer = window.setTimeout(() => { if (!card.current?.contains(document.activeElement)) primary.current?.focus() }, 300)
    return () => window.clearTimeout(timer)
  }, [ready, index])

  const back = () => setIndex((current) => Math.max(0, current - 1))
  const next = () => (last ? onClose() : setIndex((current) => current + 1))

  // Esc and the arrow keys work on the window: between steps the card is briefly away and
  // focus with it, and a quick second press must still count.
  const latest = useRef({ next, back, onClose })
  useEffect(() => { latest.current = { next, back, onClose } })
  useEffect(() => {
    const press = (event: globalThis.KeyboardEvent) => {
      if (event.key === 'Escape') { event.preventDefault(); latest.current.onClose() }
      else if (event.key === 'ArrowRight') { event.preventDefault(); latest.current.next() }
      else if (event.key === 'ArrowLeft') { event.preventDefault(); latest.current.back() }
    }
    window.addEventListener('keydown', press)
    return () => window.removeEventListener('keydown', press)
  }, [])

  function keys(event: KeyboardEvent<HTMLDivElement>) {
    if (event.key === 'Tab') {
      // Focus stays on the card's buttons.
      const buttons = [...(card.current?.querySelectorAll<HTMLButtonElement>('button:not([disabled])') ?? [])]
      if (!buttons.length) return
      const at = buttons.indexOf(document.activeElement as HTMLButtonElement)
      event.preventDefault()
      buttons[(at + (event.shiftKey ? -1 : 1) + buttons.length) % buttons.length].focus()
    }
  }

  const box = ready ? found?.box ?? null : null
  return createPortal(
    // Pressing outside the card keeps focus on it; nothing behind can be used meanwhile.
    <div className="fixed inset-0 z-50" onMouseDown={(event) => event.preventDefault()}>
      {box
        ? <div aria-hidden="true" data-testid="guide-highlight" className="pointer-events-none fixed rounded-lg shadow-[0_0_0_9999px_rgb(0_0_0/0.6)] ring-2 ring-primary transition-all duration-200 motion-reduce:transition-none" style={box} />
        : <div aria-hidden="true" className="fixed inset-0 bg-black/60" />}
      {ready ? (
        <Card ref={card} role="dialog" aria-modal="true" aria-labelledby={titleID} aria-describedby={bodyID} onKeyDown={keys}
          className="fixed w-80 max-w-[calc(100vw-2rem)] shadow-lg" style={placeCard(box, size, step.side, viewport)}>
          <CardHeader className="gap-2 p-4">
            <p className="text-xs text-muted-foreground">{index + 1} / {steps.length}</p>
            <CardTitle id={titleID} className="text-base">{zh ? step.title.zh : step.title.en}</CardTitle>
            <CardDescription id={bodyID} className="leading-relaxed">{zh ? step.body.zh : step.body.en}</CardDescription>
          </CardHeader>
          <CardFooter className="justify-between gap-2 p-4 pt-0">
            {last ? <span /> : <Button type="button" variant="ghost" size="sm" onClick={onClose}>{pick(zh, '跳过', 'Skip')}</Button>}
            <div className="flex gap-2">
              {index > 0 ? <Button type="button" variant="outline" size="sm" onClick={back}>{pick(zh, '上一步', 'Back')}</Button> : null}
              <Button ref={primary} type="button" size="sm" onClick={next}>{last ? pick(zh, '完成', 'Done') : pick(zh, '下一步', 'Next')}</Button>
            </div>
          </CardFooter>
        </Card>
      ) : null}
    </div>,
    document.body,
  )
}
