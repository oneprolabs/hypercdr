import React from 'react';
import { Calendar, Layers, Plus, Sun, Trash2, Zap } from 'lucide-react';
import { AnimatePresence, motion } from 'motion/react';
import { getUserTimeZone, userTimeZoneLabel } from '../../lib/date-time';
import { retentionUnits, type RetentionPeriod } from './retention-model';
import type { PolicyComposition, PolicyForm, PolicyScheduleType } from './policy-form-model';
const weekdays=['Sunday','Monday','Tuesday','Wednesday','Thursday','Friday','Saturday'];
function TimeSelector({hour,minute,onChange}:{hour:number;minute:number;onChange:(hour:number,minute:number)=>void}){return <div className="flex items-center gap-2"><select value={hour} onChange={event=>onChange(Number(event.target.value),minute)} className="rounded-lg border border-slate-200 bg-white px-3 py-2 text-sm font-bold outline-none">{Array.from({length:24}).map((_,index)=><option key={index} value={index}>{String(index).padStart(2,'0')}</option>)}</select><span className="font-bold text-slate-500">:</span><select value={minute} onChange={event=>onChange(hour,Number(event.target.value))} className="rounded-lg border border-slate-200 bg-white px-3 py-2 text-sm font-bold outline-none">{Array.from({length:60}).map((_,index)=><option key={index} value={index}>{String(index).padStart(2,'0')}</option>)}</select></div>}
export function PolicyFormFields({policyForm,setPolicyForm}:{policyForm:PolicyForm;setPolicyForm:React.Dispatch<React.SetStateAction<PolicyForm>>}){
 const showScheduleConfig=policyForm.composition!=='retention';
 const showRetentionConfig=policyForm.composition!=='schedule';
 const retentionTimeZone=policyForm.retentionTimezone || getUserTimeZone();
 const separateRetentionTimeZone=showScheduleConfig && showRetentionConfig && (policyForm.retentionRules || []).length > 0 && retentionTimeZone!==getUserTimeZone();
 return <div className="hbdr-policy-form-fields">
              <div className="hbdr-filter-drawer-body hbdr-policy-drawer-body">
                <section className="hbdr-advanced-filter-section">
                  <h4>Policy Name</h4>
                  <div className="hbdr-advanced-filter-box hbdr-policy-form-box">
                  <input type="text" value={policyForm.name} onChange={event => setPolicyForm({ ...policyForm, name: event.target.value })} className="w-full rounded-xl border border-slate-200 bg-slate-50 px-4 py-2.5 text-sm font-medium outline-none transition-all focus:border-indigo-500 focus:ring-4 focus:ring-indigo-100" placeholder="Example: Core workload-5minute rapid DR" />
                  </div>
                </section>

                <section className="hbdr-advanced-filter-section">
                  <div className="hbdr-policy-composition-heading">
                    <h4>Policy Composition</h4>
                    <span className="hbdr-policy-timezone-note">{userTimeZoneLabel(showScheduleConfig ? getUserTimeZone() : retentionTimeZone)}{separateRetentionTimeZone ? ` · Retention: ${retentionTimeZone}` : ''}</span>
                  </div>
                  <div className="hbdr-advanced-filter-box hbdr-policy-form-box">
                  <div className="grid gap-2 md:grid-cols-3">
                    {[
                      { id: 'combined' as PolicyComposition, title: 'Schedule + Retention', badge: 'Recommended' },
                      { id: 'schedule' as PolicyComposition, title: 'Schedule Only', badge: 'Timing' },
                      { id: 'retention' as PolicyComposition, title: 'Retention Only', badge: 'Lifecycle' },
                    ].map(item => (
                      <button
                        key={item.id}
                        type="button"
                        onClick={() => setPolicyForm({ ...policyForm, composition: item.id })}
                        aria-pressed={policyForm.composition === item.id}
                        className={`hbdr-policy-composition-card flex items-center justify-between gap-3 rounded-xl border px-3 py-2 text-left transition-all ${policyForm.composition === item.id ? 'border-indigo-500 bg-indigo-50 text-indigo-950 shadow-sm' : 'border-slate-200 bg-white text-slate-600 hover:border-indigo-200 hover:bg-slate-50'}`}
                      >
                        <span>
                          <span className={`mb-1 inline-flex rounded-full px-2 py-0.5 text-[10px] font-black ${policyForm.composition === item.id ? 'bg-indigo-600 text-white' : 'bg-slate-100 text-slate-500'}`}>{item.badge}</span>
                          <strong className="block text-sm font-black">{item.title}</strong>
                        </span>
                        <span className={`h-4 w-4 rounded-full border ${policyForm.composition === item.id ? 'border-indigo-600 bg-indigo-600' : 'border-slate-300 bg-white'}`} />
                      </button>
                    ))}
                  </div>
                  </div>
                </section>

                {showScheduleConfig && (
                <section className="hbdr-advanced-filter-section hbdr-policy-schedule-section">
                  <h4>Schedule Type</h4>
                  <div className="hbdr-advanced-filter-box hbdr-policy-form-box">
                  <div className="hbdr-policy-schedule-options grid grid-cols-2 gap-2">
                    {[
                      { id: 'interval' as PolicyScheduleType, label: 'Interval', icon: Zap },
                      { id: 'daily' as PolicyScheduleType, label: 'Daily Backup', icon: Sun },
                      { id: 'weekly' as PolicyScheduleType, label: 'Weekly Backup', icon: Calendar },
                      { id: 'monthly' as PolicyScheduleType, label: 'Monthly Backup', icon: Layers },
                    ].map(type => {
                      const TypeIcon = type.icon;
                      return (
                        <button key={type.id} onClick={() => setPolicyForm({ ...policyForm, type: type.id })} className={`flex items-center gap-2 rounded-xl border-2 px-3 py-2 transition-all ${policyForm.type === type.id ? 'border-indigo-600 bg-indigo-50/50 shadow-sm' : 'border-slate-100 hover:border-slate-200'}`}>
                          <span className={`rounded-lg p-1.5 ${policyForm.type === type.id ? 'bg-indigo-600 text-white' : 'bg-slate-100 text-slate-500'}`}><TypeIcon size={16} /></span>
                          <span className={`text-sm font-bold ${policyForm.type === type.id ? 'text-indigo-900' : 'text-slate-600'}`}>{type.label}</span>
                        </button>
                      );
                    })}
                  </div>

                  <AnimatePresence mode="wait">
                    <motion.div key={policyForm.type} initial={{ opacity: 0, y: 10 }} animate={{ opacity: 1, y: 0 }} exit={{ opacity: 0, y: -10 }} className="rounded-xl border border-slate-100 bg-slate-50 p-3">
                      {policyForm.type === 'interval' && (
                        <div className="flex flex-wrap items-center gap-3">
                          <span className="text-sm font-bold text-slate-700">Run every: </span>
                          <input type="number" min={1} value={policyForm.intervalValue} onChange={event => setPolicyForm({ ...policyForm, intervalValue: Number(event.target.value) })} className="w-20 rounded-lg border border-slate-200 bg-white px-3 py-2 text-center text-sm font-bold outline-none focus:border-indigo-500" />
                          <select value={policyForm.intervalUnit} onChange={event => setPolicyForm({ ...policyForm, intervalUnit: event.target.value as 'minutes' | 'hours' })} className="rounded-lg border border-slate-200 bg-white px-3 py-2 text-sm font-bold outline-none">
                            <option value="minutes">minutes</option>
                            <option value="hours">hours</option>
                          </select>
                        </div>
                      )}
                      {policyForm.type === 'daily' && (
                        <div className="flex flex-wrap items-center gap-4">
                          <span className="text-sm font-bold text-slate-700">Every day at</span>
                          <TimeSelector hour={policyForm.hour} minute={policyForm.minute} onChange={(hour, minute) => setPolicyForm({ ...policyForm, hour, minute })} />
                        </div>
                      )}
                      {policyForm.type === 'weekly' && (
                        <div className="flex flex-wrap items-center gap-4">
                          <span className="text-sm font-bold text-slate-700">Every</span>
                          <select value={policyForm.weekDay} onChange={event => setPolicyForm({ ...policyForm, weekDay: Number(event.target.value) })} className="rounded-lg border border-slate-200 bg-white px-3 py-2 text-sm font-bold outline-none">
                            {weekdays.map((day, index) => <option key={day} value={index}>{day}</option>)}
                          </select>
                          <span className="text-sm font-bold text-slate-700">at</span>
                          <TimeSelector hour={policyForm.hour} minute={policyForm.minute} onChange={(hour, minute) => setPolicyForm({ ...policyForm, hour, minute })} />
                        </div>
                      )}
                      {policyForm.type === 'monthly' && (
                        <div className="flex flex-wrap items-center gap-4">
                          <span className="text-sm font-bold text-slate-700">Every month on day</span>
                          <select value={policyForm.monthDay} onChange={event => setPolicyForm({ ...policyForm, monthDay: Number(event.target.value) })} className="rounded-lg border border-slate-200 bg-white px-3 py-2 text-sm font-bold outline-none">
                            {Array.from({ length: 31 }).map((_, index) => <option key={index + 1} value={index + 1}>{index + 1}</option>)}
                          </select>
                          <span className="text-sm font-bold text-slate-700">at</span>
                          <TimeSelector hour={policyForm.hour} minute={policyForm.minute} onChange={(hour, minute) => setPolicyForm({ ...policyForm, hour, minute })} />
                        </div>
                      )}
                    </motion.div>
                  </AnimatePresence>
                  </div>
                </section>
                )}

                {showRetentionConfig && (
                <section className="hbdr-advanced-filter-section">
                  <div className="hbdr-policy-quota-header">
                    <h4><label htmlFor="policy-restore-point-quota">Restore Point Quota</label></h4>
                    <input id="policy-restore-point-quota" type="number" min={1} value={policyForm.retention} onChange={event => setPolicyForm({ ...policyForm, retention: Number(event.target.value) })} className="w-20 rounded-lg border border-slate-200 bg-white px-3 py-2 text-sm outline-none focus:border-indigo-500" />
                    <span className="text-xs text-slate-500">restore points</span>
                  </div>
                  <p className="hbdr-policy-timezone-note">All retained points count toward the quota.</p>
                  <div className="hbdr-retention-heading"><h4>Automatic Retention <span>Optional</span></h4><button type="button" disabled={(policyForm.retentionRules || []).length >= 32} onClick={() => setPolicyForm({ ...policyForm, retentionRules: [...(policyForm.retentionRules || []), { period: 'daily', ordinal: 1, keep: 3 }], retentionTimezone: policyForm.retentionTimezone || getUserTimeZone() })}><Plus size={14} />Add Rule</button></div>
                  {(policyForm.retentionRules || []).length > 0 ? <>
                    <div className="hbdr-retention-rules">
                      <div className="hbdr-retention-rule-header"><span>Period</span><span>Successful point</span><span>Keep for</span><span /></div>
                      {(policyForm.retentionRules || []).map((rule, index) => <div className="hbdr-retention-rule" key={index}>
                        <select aria-label={`Rule ${index + 1} period`} value={rule.period} onChange={event => setPolicyForm({ ...policyForm, retentionRules: policyForm.retentionRules?.map((item, position) => position === index ? { ...item, period: event.target.value as RetentionPeriod } : item) })}>{Object.entries({ daily: 'Daily', weekly: 'Weekly', monthly: 'Monthly', yearly: 'Yearly' }).map(([value, label]) => <option key={value} value={value}>{label}</option>)}</select>
                        <label>#{' '}<input aria-label={`Rule ${index + 1} point number`} type="number" min={1} max={100000} value={rule.ordinal} onChange={event => setPolicyForm({ ...policyForm, retentionRules: policyForm.retentionRules?.map((item, position) => position === index ? { ...item, ordinal: Number(event.target.value) } : item) })} /></label>
                        <label><input aria-label={`Rule ${index + 1} duration`} type="number" min={1} max={1000} value={rule.keep} onChange={event => setPolicyForm({ ...policyForm, retentionRules: policyForm.retentionRules?.map((item, position) => position === index ? { ...item, keep: Number(event.target.value) } : item) })} />{retentionUnits[rule.period]}</label>
                        <button type="button" aria-label={`Remove rule ${index + 1}`} onClick={() => setPolicyForm({ ...policyForm, retentionRules: policyForm.retentionRules?.filter((_, position) => position !== index) })}><Trash2 size={14} /></button>
                      </div>)}
                    </div>
                    <p className="hbdr-policy-timezone-note">Includes the current period.{policyForm.retentionRules?.some(rule => rule.period === 'weekly') ? ' Weeks start Monday.' : ''}</p>
                  </> : <p className="hbdr-policy-timezone-note">No rules: replace oldest eligible points when full.</p>}
                </section>
                )}
              </div>

 </div>;
}
