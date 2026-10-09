import React from 'react';
import { AnimatePresence, motion } from 'motion/react';
import { Database, MoreVertical, Grid3X3, RefreshCw, X } from 'lucide-react';
import type { AppItem, ResourceCategory, ResourceCategoryKey, ResourceKindSummary } from '../clusters/types';
import { formatAge, formatBytes, resourceCategoryIconMap, resourceCategoryMeta, resourceInventoryDetailText, resourceInventoryTitle, shortResourceKind } from '../recovery/task-ui';

export default function NamespaceResourcesDrawer({ resourceDetail, resourceDetailGroups, currentClusterId, resourceRefreshKey, resourceRefreshStatus, onClose, onRefresh }: {
 resourceDetail: { app: AppItem } | null;
 resourceDetailGroups: Array<{ category: ResourceCategory; items: ResourceKindSummary[] }>;
 currentClusterId: string | null;
 resourceRefreshKey: string;
 resourceRefreshStatus: { key: string; status: string; message?: string } | null;
 onClose: () => void;
 onRefresh: (app: AppItem) => Promise<unknown>;
}) {
 return (
      <AnimatePresence>
        {resourceDetail && (
          <div className="fixed inset-0 z-[230]">
            {(() => {
              const namespace = resourceDetail.app.namespace || resourceDetail.app.name;
              const clusterId = resourceDetail.app.clusterId || currentClusterId || '';
              const isRefreshing = resourceRefreshKey === `${clusterId}:${namespace}`;
              const refreshState = resourceRefreshStatus?.key === `${clusterId}:${namespace}` ? resourceRefreshStatus : null;
              return (
                <>
            <motion.div
              initial={{ opacity: 0 }}
              animate={{ opacity: 1 }}
              exit={{ opacity: 0 }}
              className="hbdr-filter-drawer-backdrop"
              onClick={() => { onClose(); }}
            />
            <motion.div
              initial={{ opacity: 0, x: 32 }}
              animate={{ opacity: 1, x: 0 }}
              exit={{ opacity: 0, x: 32 }}
              transition={{ duration: 0.16, ease: 'easeOut' }}
              className="hbdr-filter-drawer hbdr-resource-drawer"
            >
              <div className="hbdr-filter-drawer-head hbdr-resource-drawer-head">
                <div className="hbdr-resource-modal-title">
                  <span className="hbdr-resource-modal-icon"><Grid3X3 size={18} /></span>
                  <div>
                    <h3>Namespace Resources</h3>
                    <p>{resourceDetail.app.namespace || resourceDetail.app.name}</p>
                  </div>
                </div>
                <div className="hbdr-resource-modal-head-right">
                  <div className="hbdr-resource-modal-meta">
                    <span>
                      <em>Groups</em>
                      <strong>{resourceDetailGroups.length}</strong>
                    </span>
                    <span>
                      <em>Objects</em>
                      <strong>{resourceDetailGroups.reduce((sum, category) => sum + category.category.total, 0)}</strong>
                    </span>
                    <span>
                      <em>Storage</em>
                      <strong>{resourceDetail.app.pvCapacityBytes ? formatBytes(resourceDetail.app.pvCapacityBytes) : '0 B'}</strong>
                    </span>
                  </div>
                  <button
                    type="button"
                    onClick={() => void onRefresh(resourceDetail.app)}
                    disabled={isRefreshing}
                    title="Refresh namespace resources from the cluster agent"
                    aria-label="Refresh namespace resources"
                  >
                    <RefreshCw size={18} className={isRefreshing ? 'animate-spin' : ''} />
                  </button>
              <button type="button" onClick={() => { onClose(); }} aria-label="Close resource details"><X size={18} /></button>
                </div>
              </div>
              <div className="hbdr-filter-drawer-body hbdr-resource-detail">
                {refreshState && refreshState.status !== 'succeeded' && (
                  <div className={`hbdr-resource-refresh-note ${refreshState.status === 'pending' ? 'is-pending' : 'is-error'}`} role="status">
                    {refreshState.status === 'pending' ? 'Refreshing inventory…' : (refreshState.message || 'Inventory refresh failed.')}
                  </div>
                )}
                {resourceDetailGroups.length > 0 ? (
                  <div className="hbdr-resource-inventory">
                    <div className="hbdr-resource-overview-strip">
                      {resourceDetailGroups.map(group => {
                        const Icon = resourceCategoryIconMap[group.category.key as ResourceCategoryKey] || MoreVertical;
                        return (
                          <span key={group.category.key}>
                            <Icon size={15} />
                            <strong>{group.category.total}</strong>
                            <em>{group.category.label}</em>
                          </span>
                        );
                      })}
                    </div>
                    {resourceDetailGroups.map(group => {
                      const Icon = resourceCategoryIconMap[group.category.key as ResourceCategoryKey] || MoreVertical;
                      return (
                        <section key={group.category.key} className="hbdr-resource-inventory-section">
                          <div className="hbdr-resource-inventory-divider">
                            <span><Icon size={14} />{group.category.label}</span>
                            <em>{group.category.total} objects</em>
                          </div>
                          <div className="hbdr-resource-inventory-table">
                            <div className="hbdr-resource-inventory-columns">
                              <span>Resource</span>
                              <span>Type</span>
                              <span>Details</span>
                            </div>
                            {group.items.flatMap(item => {
                              const rows = item.resources || [];
                              if (rows.length === 0) {
                                return [(
                                  <div key={`${group.category.key}-${item.kind}-count`} className="hbdr-resource-inventory-count-row">
                                    <span className="hbdr-resource-kind-badge">{item.shortName || shortResourceKind(item.kind)}</span>
                                    <strong>{item.kind}</strong>
                                    <em>{item.count} reported, object details pending</em>
                                  </div>
                                )];
                              }
                              return rows.map(resource => {
                                const detailText = resourceInventoryDetailText(resource, item, resourceDetail.app.namespace);
                                return (
                                  <div
                                    key={`${item.kind}-${resource.namespace || resourceDetail.app.namespace}-${resource.name}`}
                                    className="hbdr-resource-inventory-item"
                                    title={resourceInventoryTitle(resource, item, resourceDetail.app.namespace)}
                                  >
                                    <div className="hbdr-resource-inventory-resource">
                                      <span className="hbdr-resource-kind-badge">{item.shortName || shortResourceKind(item.kind)}</span>
                                      <div>
                                        <strong>{resource.name}</strong>
                                        <em>{resource.namespace || resourceDetail.app.namespace} · {formatAge(resource.ageSeconds)}</em>
                                      </div>
                                    </div>
                                    <div className="hbdr-resource-inventory-type">{item.kind}</div>
                                    <div className="hbdr-resource-inventory-detail-text">{detailText}</div>
                                  </div>
                                );
                              });
                            })}
                          </div>
                        </section>
                      );
                    })}
                  </div>
                ) : (
                  <div className="hbdr-resource-detail-empty">
                    <Database size={18} />
                    <strong>No namespaced resources found</strong>
                    <span>The agent has not reported resources for this namespace yet.</span>
                  </div>
                )}
              </div>
            </motion.div>
                </>
              );
            })()}
          </div>
        )}
      </AnimatePresence>

 );
}
