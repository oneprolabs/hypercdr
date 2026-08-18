import React from 'react';
import {
  flexRender,
  getCoreRowModel,
  getPaginationRowModel,
  getSortedRowModel,
  useReactTable,
  type ColumnResizeMode,
  type PaginationState,
  type SortingState,
} from '@tanstack/react-table';
import type { HyperTableColumn, HyperTableDensity, HyperTableVariant } from './types';

type HyperTableProps<TData> = {
  columns: HyperTableColumn<TData>[];
  data: TData[];
  getRowId?: (row: TData, index: number) => string;
  variant?: HyperTableVariant;
  density?: HyperTableDensity;
  emptyMessage?: string;
  onRowClick?: (row: TData) => void;
  getRowClassName?: (row: TData) => string;
  renderExpandedRow?: (row: TData) => React.ReactNode;
  className?: string;
  pagination?: boolean;
  initialPageSize?: number;
  pageSizeOptions?: number[];
  selectedCount?: number;
  resetPageOnDataChange?: boolean;
};

const columnResizeMode: ColumnResizeMode = 'onChange';

type HyperColumnMeta<TData> = {
  align?: 'left' | 'center' | 'right';
  className?: string;
  kind?: 'primary' | 'secondary' | 'number' | 'code' | 'status';
  title?: (row: TData) => string;
};

export function HyperTable<TData>(props: HyperTableProps<TData>) {
  const {
    columns,
    data,
    getRowId,
    variant = 'page',
    density = 'comfortable',
    emptyMessage = 'No data available.',
    onRowClick,
    getRowClassName,
    renderExpandedRow,
    className = '',
    pagination: paginationEnabled = true,
    initialPageSize = 10,
    pageSizeOptions = [10, 20, 50],
    selectedCount,
    resetPageOnDataChange = false,
  } = props;
  const [sorting, setSorting] = React.useState<SortingState>([]);
  const [pagination, setPagination] = React.useState<PaginationState>({
    pageIndex: 0,
    pageSize: initialPageSize,
  });

  React.useEffect(() => {
    setPagination(prev => ({ ...prev, pageIndex: 0 }));
  }, [sorting]);

  React.useEffect(() => {
    if (resetPageOnDataChange) {
      setPagination(prev => ({ ...prev, pageIndex: 0 }));
      return;
    }
    setPagination(prev => {
      const maxPageIndex = Math.max(0, Math.ceil(data.length / prev.pageSize) - 1);
      return prev.pageIndex > maxPageIndex ? { ...prev, pageIndex: maxPageIndex } : prev;
    });
  }, [data.length, resetPageOnDataChange]);

  const table = useReactTable({
    data,
    columns,
    getRowId,
    state: { sorting, pagination },
    onSortingChange: setSorting,
    onPaginationChange: setPagination,
    getCoreRowModel: getCoreRowModel(),
    getSortedRowModel: getSortedRowModel(),
    getPaginationRowModel: paginationEnabled ? getPaginationRowModel() : undefined,
    columnResizeMode,
    defaultColumn: {
      enableSorting: true,
      enableResizing: true,
      minSize: 72,
      size: 160,
      maxSize: 640,
    },
  });

  const totalWidth = table.getTotalSize();
  const visibleRows = paginationEnabled ? table.getPaginationRowModel().rows : table.getRowModel().rows;
  const pageCount = Math.max(1, table.getPageCount());
  const pageIndex = table.getState().pagination.pageIndex;
  const pageSize = table.getState().pagination.pageSize;
  const totalRows = data.length;
  const rangeStart = totalRows === 0 ? 0 : pageIndex * pageSize + 1;
  const rangeEnd = Math.min(totalRows, (pageIndex + 1) * pageSize);
  const classes = [
    'hbdr-hyper-table',
    `hbdr-hyper-table-${variant}`,
    `hbdr-hyper-table-${density}`,
    onRowClick ? 'hbdr-hyper-table-clickable' : '',
    className,
  ].filter(Boolean).join(' ');

  return (
    <div className={classes}>
      <div className="hbdr-hyper-table-scroll">
        <div className="hbdr-hyper-table-inner" style={{ width: totalWidth }}>
          <div className="hbdr-hyper-table-head">
            {table.getHeaderGroups().map(headerGroup => (
              <div key={headerGroup.id} className="hbdr-hyper-table-row">
                {headerGroup.headers.map(header => {
                  const sortState = header.column.getIsSorted();
                  const canSort = header.column.getCanSort();
                  const canResize = header.column.getCanResize();
                  const meta = header.column.columnDef.meta as HyperColumnMeta<TData> | undefined;
                  return (
                    <div
                      key={header.id}
                      className={`hbdr-hyper-table-cell hbdr-hyper-table-head-cell hbdr-hyper-table-align-${meta?.align || 'left'} ${canSort ? 'is-sortable' : ''} ${sortState ? 'is-sorted' : ''}`}
                      style={{ width: header.getSize() }}
                    >
                      {canSort ? (
                        <button type="button" onClick={header.column.getToggleSortingHandler()}>
                          <span>{header.isPlaceholder ? null : flexRender(header.column.columnDef.header, header.getContext())}</span>
                          <em aria-hidden="true">{sortState === 'asc' ? '▲' : sortState === 'desc' ? '▼' : '↕'}</em>
                        </button>
                      ) : (
                        <div className="hbdr-hyper-table-static-head">
                          {header.isPlaceholder ? null : flexRender(header.column.columnDef.header, header.getContext())}
                        </div>
                      )}
                      {canResize && (
                        <span
                          className={`hbdr-hyper-table-resizer ${header.column.getIsResizing() ? 'is-resizing' : ''}`}
                          onMouseDown={header.getResizeHandler()}
                          onTouchStart={header.getResizeHandler()}
                        />
                      )}
                    </div>
                  );
                })}
              </div>
            ))}
          </div>
          <div className="hbdr-hyper-table-body">
            {visibleRows.length > 0 ? visibleRows.map(row => (
              <React.Fragment key={row.id}>
                <div
                  className={`hbdr-hyper-table-row ${getRowClassName?.(row.original) || ''}`}
                  onClick={() => onRowClick?.(row.original)}
                >
                  {row.getVisibleCells().map(cell => {
                    const meta = cell.column.columnDef.meta as HyperColumnMeta<TData> | undefined;
                    const title = meta?.title?.(row.original);
                    return (
                      <div
                        key={cell.id}
                        className={`hbdr-hyper-table-cell hbdr-hyper-table-body-cell hbdr-hyper-table-cell-${meta?.kind || 'default'} hbdr-hyper-table-align-${meta?.align || 'left'} ${meta?.className || ''}`}
                        style={{ width: cell.column.getSize() }}
                        title={title}
                      >
                        {flexRender(cell.column.columnDef.cell, cell.getContext())}
                      </div>
                    );
                  })}
                </div>
                {renderExpandedRow?.(row.original)}
              </React.Fragment>
            )) : (
              <div className="hbdr-hyper-table-empty" style={{ width: totalWidth }}>{emptyMessage}</div>
            )}
          </div>
        </div>
      </div>
      {paginationEnabled && totalRows > 0 && (
        <div className="hbdr-hyper-table-pagination">
          <div className="hbdr-hyper-table-pagination-summary">
            <span>{rangeStart}-{rangeEnd} of {totalRows}</span>
            {typeof selectedCount === 'number' && (
              <span className={selectedCount > 0 ? 'is-active' : ''}>{selectedCount} selected</span>
            )}
          </div>
          <div className="hbdr-hyper-table-pagination-controls">
            <label>
              Rows
              <select
                value={pageSize}
                onChange={event => table.setPageSize(Number(event.target.value))}
              >
                {pageSizeOptions.map(size => <option key={size} value={size}>{size}</option>)}
              </select>
            </label>
            <button type="button" onClick={() => table.previousPage()} disabled={!table.getCanPreviousPage()}>Prev</button>
            <span>{pageIndex + 1} / {pageCount}</span>
            <button type="button" onClick={() => table.nextPage()} disabled={!table.getCanNextPage()}>Next</button>
          </div>
        </div>
      )}
    </div>
  );
}
