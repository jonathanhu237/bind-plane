import {
  flexRender,
  getCoreRowModel,
  getFilteredRowModel,
  getSortedRowModel,
  useReactTable,
  type Column,
  type ColumnDef,
  type OnChangeFn,
  type SortingState,
} from '@tanstack/react-table'
import { ArrowDown, ArrowUp, ArrowUpDown, Search, X } from 'lucide-react'
import { useMemo } from 'react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'

const EMPTY_SORTING: SortingState = []

export function SortableHeader<T>({ column, children }: { column: Column<T, unknown>; children: React.ReactNode }) {
  const { t } = useTranslation('common')
  const direction = column.getIsSorted()
  const Icon = direction === 'asc' ? ArrowUp : direction === 'desc' ? ArrowDown : ArrowUpDown
  const label = direction === 'asc' ? t('sortDescending') : t('sortAscending')
  return (
    <Button type="button" variant="ghost" size="sm" className="-ml-3 h-8 gap-1.5 px-3" onClick={column.getToggleSortingHandler()} title={label}>
      {children}
      <Icon aria-hidden="true" />
    </Button>
  )
}

type DataTableProps<T> = {
  columns: ColumnDef<T, unknown>[]
  data: T[]
  search: string
  onSearchChange: (value: string) => void
  searchPlaceholder: string
  clearSearchLabel: string
  emptyMessage: string
  sorting?: SortingState
  onSortingChange?: OnChangeFn<SortingState>
  manualFiltering?: boolean
  manualSorting?: boolean
  toolbar?: React.ReactNode
}

export function DataTable<T>({
  columns,
  data,
  search,
  onSearchChange,
  searchPlaceholder,
  clearSearchLabel,
  emptyMessage,
  sorting = EMPTY_SORTING,
  onSortingChange,
  manualFiltering = false,
  manualSorting = false,
  toolbar,
}: DataTableProps<T>) {
  const stableColumns = useMemo(() => columns, [columns])
  const table = useReactTable({
    data,
    columns: stableColumns,
    state: { globalFilter: search, sorting },
    onGlobalFilterChange: (updater) => {
      const next = typeof updater === 'function' ? updater(search) : updater
      onSearchChange(next)
    },
    onSortingChange,
    enableSortingRemoval: false,
    getCoreRowModel: getCoreRowModel(),
    getFilteredRowModel: manualFiltering ? undefined : getFilteredRowModel(),
    getSortedRowModel: manualSorting ? undefined : getSortedRowModel(),
    manualFiltering,
    manualSorting,
  })
  const rows = table.getRowModel().rows

  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between">
        <div className="relative w-full sm:max-w-sm">
          <Search aria-hidden="true" className="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-muted-foreground" />
          <Input
            value={search}
            onChange={(event) => onSearchChange(event.target.value)}
            placeholder={searchPlaceholder}
            aria-label={searchPlaceholder}
            className="pl-9"
          />
        </div>
        <div className="flex flex-wrap items-center justify-end gap-2">
          {toolbar}
          {search ? (
            <Button type="button" variant="ghost" size="sm" onClick={() => onSearchChange('')}>
              <X aria-hidden="true" data-icon="inline-start" />
              {clearSearchLabel}
            </Button>
          ) : null}
        </div>
      </div>
      <div className="overflow-x-auto rounded-md border">
        <Table>
          <TableHeader>
            {table.getHeaderGroups().map((headerGroup) => (
              <TableRow key={headerGroup.id}>
                {headerGroup.headers.map((header) => (
                  <TableHead key={header.id}>
                    {header.isPlaceholder ? null : flexRender(header.column.columnDef.header, header.getContext())}
                  </TableHead>
                ))}
              </TableRow>
            ))}
          </TableHeader>
          <TableBody>
            {rows.length > 0 ? rows.map((row) => (
              <TableRow key={row.id}>
                {row.getVisibleCells().map((cell) => <TableCell key={cell.id}>{flexRender(cell.column.columnDef.cell, cell.getContext())}</TableCell>)}
              </TableRow>
            )) : (
              <TableRow>
                <TableCell colSpan={columns.length} className="h-24 text-center text-muted-foreground">{emptyMessage}</TableCell>
              </TableRow>
            )}
          </TableBody>
        </Table>
      </div>
    </div>
  )
}
