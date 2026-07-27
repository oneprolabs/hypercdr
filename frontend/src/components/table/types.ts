import type { ColumnDef } from '@tanstack/react-table';

export type HyperTableVariant = 'page' | 'modal';
export type HyperTableDensity = 'comfortable' | 'compact';

export type HyperTableColumn<TData> = ColumnDef<TData, unknown> & {
  meta?: {
    align?: 'left' | 'center' | 'right';
    className?: string;
    title?: (row: TData) => string;
  };
};
