import React, { useEffect, useState } from 'react';
import { Archive, Check, Filter, RefreshCw, Settings, X } from 'lucide-react';
import { AnimatePresence, motion } from 'motion/react';

export type ListToolbarChoice = { value:string; label:string; count?:number };
export type ListToolbarColumn = { value:string; label:string; locked?:boolean };
const COLUMN_FILTER_PREFIX='columnFilter:';
const toggleListValue=(values:string[],value:string)=>values.includes(value)?values.filter(item=>item!==value):[...values,value];
const makeColumnFilterToken=(field:string,value:string)=>`${COLUMN_FILTER_PREFIX}${encodeURIComponent(field)}:${encodeURIComponent(value.trim())}`;
export const parseColumnFilterToken=(token:string)=>{if(!token.startsWith(COLUMN_FILTER_PREFIX))return null;const body=token.slice(COLUMN_FILTER_PREFIX.length);const separator=body.indexOf(':');if(separator<0)return null;const field=decodeURIComponent(body.slice(0,separator));const value=decodeURIComponent(body.slice(separator+1)).trim();return field&&value?{field,value}:null};
export const matchesColumnFilterToken=(token:string,valueForField:(field:string)=>string)=>{const parsed=parseColumnFilterToken(token);return parsed?valueForField(parsed.field).toLowerCase().includes(parsed.value.toLowerCase()):false};
export const listToolbarQueryFields=(fixedFields:ListToolbarChoice[],columns:ListToolbarColumn[],visibleColumns:string[])=>{const seen=new Set<string>();const fields:ListToolbarChoice[]=[];const append=(field:ListToolbarChoice)=>{if(!seen.has(field.value)){seen.add(field.value);fields.push(field)}};fixedFields.forEach(append);columns.filter(column=>visibleColumns.includes(column.value)).forEach(column=>append({value:column.value,label:column.label}));return fields};

export default function ListToolbarControls(props: {
  query: string;
  setQuery: (value: string) => void;
  queryField: string;
  setQueryField: (value: string) => void;
  queryFields: ListToolbarChoice[];
  tags: ListToolbarChoice[];
  activeTags: string[];
  setActiveTags: React.Dispatch<React.SetStateAction<string[]>>;
  filters: ListToolbarChoice[];
  activeFilters: string[];
  setActiveFilters: React.Dispatch<React.SetStateAction<string[]>>;
  columns: ListToolbarColumn[];
  visibleColumns: string[];
  setVisibleColumns: React.Dispatch<React.SetStateAction<string[]>>;
  onRefresh: () => void;
}) {
  const {
    query,
    setQuery,
    queryField,
    setQueryField,
    queryFields,
    tags,
    activeTags,
    setActiveTags,
    activeFilters,
    setActiveFilters,
    columns,
    visibleColumns,
    setVisibleColumns,
    onRefresh,
  } = props;
  const [openPanel, setOpenPanel] = useState<'tags' | 'filters' | 'columns' | null>(null);
  const [draftColumnFilters, setDraftColumnFilters] = useState<Record<string, string>>({});
  const activeBadgeCount = activeTags.length + activeFilters.length;
  const resetListTools = () => {
    setQuery('');
    setActiveTags([]);
    setActiveFilters([]);
  };

  useEffect(() => {
    if (queryFields.length > 0 && !queryFields.some(field => field.value === queryField)) {
      setQueryField(queryFields[0].value);
    }
  }, [queryField, queryFields, setQueryField]);

  useEffect(() => {
    if (openPanel !== 'filters') return;
    const nextColumnFilters: Record<string, string> = {};
    activeFilters.forEach(filter => {
      const parsed = parseColumnFilterToken(filter);
      if (parsed) {
        nextColumnFilters[parsed.field] = parsed.value;
      }
    });
    setDraftColumnFilters(nextColumnFilters);
  }, [activeFilters, openPanel]);

  const drawerFields = queryFields;
  const activeDraftCount = Object.values(draftColumnFilters).filter(value => value.trim()).length;
  const updateDraftColumnFilter = (field: string, value: string) => setDraftColumnFilters(prev => ({ ...prev, [field]: value }));
  const clearDraftColumnFilter = (field: string) => {
    setDraftColumnFilters(prev => {
      const next = { ...prev };
      delete next[field];
      return next;
    });
  };
  const submitAdvancedFilters = () => {
    const columnTokens = queryFields.flatMap(field => {
      const value = (draftColumnFilters[field.value] || '').trim();
      return value ? [makeColumnFilterToken(field.value, value)] : [];
    });
    setActiveFilters(columnTokens);
    setOpenPanel(null);
  };
  const resetAdvancedFilters = () => {
    setDraftColumnFilters({});
    setActiveFilters([]);
  };

  return (
    <div className="hbdr-dr-query-group">
      <select aria-label="Query Field" value={queryField} onChange={event => setQueryField(event.target.value)}>
        {queryFields.map(field => <option key={field.value} value={field.value}>{field.label}</option>)}
      </select>
      <label className="hbdr-dr-search"><input value={query} onChange={event => setQuery(event.target.value)} placeholder="Enter search text" /></label>
      <button
        type="button"
        onClick={() => {
          resetListTools();
          onRefresh();
        }}
        title="Refresh"
      >
        <RefreshCw size={18} />
      </button>
      <div className="hbdr-list-tool">
        <button type="button" title="Tags" className={activeTags.length ? 'is-active' : ''} onClick={() => setOpenPanel(openPanel === 'tags' ? null : 'tags')}>
          <Archive size={18} />
        </button>
        <AnimatePresence>
          {openPanel === 'tags' && (
            <>
              <div className="hbdr-list-tool-backdrop" onClick={() => setOpenPanel(null)} />
              <motion.div initial={{ opacity: 0, y: 8, scale: 0.98 }} animate={{ opacity: 1, y: 0, scale: 1 }} exit={{ opacity: 0, y: 8, scale: 0.98 }} className="hbdr-list-tool-popover">
                <div className="hbdr-list-tool-head">
                  <strong>Tags</strong>
                  <button type="button" onClick={() => setActiveTags([])}>Clear</button>
                </div>
                <div className="hbdr-list-tool-options">
                  {tags.map(tag => (
                    <button key={tag.value} type="button" className={activeTags.includes(tag.value) ? 'is-selected' : ''} onClick={() => setActiveTags(prev => toggleListValue(prev, tag.value))}>
                      <span>{tag.label}</span>
                      {typeof tag.count === 'number' && <em>{tag.count}</em>}
                    </button>
                  ))}
                </div>
              </motion.div>
            </>
          )}
        </AnimatePresence>
      </div>
      <div className="hbdr-list-tool">
        <button type="button" title="Filter" className={activeFilters.length ? 'is-active' : ''} onClick={() => setOpenPanel(openPanel === 'filters' ? null : 'filters')}>
          <Filter size={18} />
        </button>
        <AnimatePresence>
          {openPanel === 'filters' && (
            <>
              <div className="hbdr-filter-drawer-backdrop" onClick={() => setOpenPanel(null)} />
              <motion.aside
                initial={{ opacity: 0, x: 24 }}
                animate={{ opacity: 1, x: 0 }}
                exit={{ opacity: 0, x: 24 }}
                className="hbdr-filter-drawer"
              >
                <div className="hbdr-filter-drawer-head">
                  <strong>Advanced Filter</strong>
                </div>
                <div className="hbdr-filter-drawer-body">
                  <section className="hbdr-advanced-filter-section">
                    <h4><Filter size={15} />Filter Criteria</h4>
                    <div className="hbdr-advanced-filter-box">
                      {drawerFields.map(field => (
                        <div key={field.value} className="hbdr-advanced-filter-row">
                          <label>{field.label}</label>
                          <input value={draftColumnFilters[field.value] || ''} onChange={event => updateDraftColumnFilter(field.value, event.target.value)} placeholder="Please Enter" />
                          <button type="button" onClick={() => clearDraftColumnFilter(field.value)} title="Clear">
                            <X size={13} />
                          </button>
                        </div>
                      ))}
                    </div>
                  </section>
                </div>
                <div className="hbdr-filter-drawer-actions">
                  <button type="button" onClick={submitAdvancedFilters}>Submit</button>
                  <button type="button" onClick={resetAdvancedFilters}>Reset</button>
                  <button type="button" onClick={() => setOpenPanel(null)}>Cancel</button>
                  {activeDraftCount > 0 && <span>{activeDraftCount} criteria</span>}
                </div>
              </motion.aside>
            </>
          )}
        </AnimatePresence>
      </div>
      <div className="hbdr-list-tool">
        <button type="button" title="Column Settings" className={visibleColumns.length < columns.length ? 'is-active' : ''} onClick={() => setOpenPanel(openPanel === 'columns' ? null : 'columns')}>
          <Settings size={18} />
        </button>
        <AnimatePresence>
          {openPanel === 'columns' && (
            <>
              <div className="hbdr-list-tool-backdrop" onClick={() => setOpenPanel(null)} />
              <motion.div initial={{ opacity: 0, y: 8, scale: 0.98 }} animate={{ opacity: 1, y: 0, scale: 1 }} exit={{ opacity: 0, y: 8, scale: 0.98 }} className="hbdr-list-tool-popover hbdr-list-tool-popover-wide">
                <div className="hbdr-list-tool-head">
                  <strong>Columns</strong>
                  <button type="button" onClick={() => setVisibleColumns(columns.map(column => column.value))}>Reset</button>
                </div>
                <div className="hbdr-list-tool-options">
                  {columns.map(column => (
                    <button
                      key={column.value}
                      type="button"
                      disabled={column.locked}
                      className={visibleColumns.includes(column.value) ? 'is-selected' : ''}
                      onClick={() => {
                        if (column.locked) return;
                        setVisibleColumns(prev => prev.includes(column.value) ? prev.filter(item => item !== column.value) : [...prev, column.value]);
                      }}
                    >
                      <span>{column.label}</span>
                      {column.locked ? <em>Fixed</em> : visibleColumns.includes(column.value) ? <Check size={13} /> : null}
                    </button>
                  ))}
                </div>
              </motion.div>
            </>
          )}
        </AnimatePresence>
      </div>
      {activeBadgeCount > 0 && <button type="button" className="hbdr-list-tool-reset" onClick={resetListTools}>Clear {activeBadgeCount}</button>}
    </div>
  );
}
