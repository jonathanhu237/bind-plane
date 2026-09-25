import type { ReactNode } from 'react'

export function SettingsSection({ id, title, children }: { id: string; title: string; children: ReactNode }) {
  return <section aria-labelledby={id} className="grid min-w-0 gap-5 lg:grid-cols-[12rem_minmax(0,1fr)] lg:gap-10">
    <h2 id={id} tabIndex={-1} className="scroll-mt-6 text-base font-semibold leading-6 focus:outline-none">{title}</h2>
    <div className="min-w-0">{children}</div>
  </section>
}
