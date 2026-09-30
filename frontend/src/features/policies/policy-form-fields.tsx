import React from 'react';
import { Calendar, Layers, Sun, Zap } from 'lucide-react';
import { AnimatePresence, motion } from 'motion/react';
import { userTimeZoneLabel } from '../../lib/date-time';
import type { PolicyComposition, PolicyForm, PolicyScheduleType } from './policy-form-model';
const weekdays=['Sunday','Monday','Tuesday','Wednesday','Thursday','Friday','Saturday'];
function TimeSelector({hour,minute,onChange}:{hour:number;minute:number;onChange:(hour:number,minute:number)=>void}){return <div className="flex items-center gap-2"><select value={hour} onChange={event=>onChange(Number(event.target.value),minute)} className="rounded-lg border border-slate-200 bg-white px-3 py-2 text-sm font-bold outline-none">{Array.from({length:24}).map((_,index)=><option key={index} value={index}>{String(index).padStart(2,'0')}</option>)}</select><span className="font-bold text-slate-500">:</span><select value={minute} onChange={event=>onChange(hour,Number(event.target.value))} className="rounded-lg border border-slate-200 bg-white px-3 py-2 text-sm font-bold outline-none">{Array.from({length:60}).map((_,index)=><option key={index} value={index}>{String(index).padStart(2,'0')}</option>)}</select></div>}
export function PolicyFormFields({policyForm,setPolicyForm}:{policyForm:PolicyForm;setPolicyForm:React.Dispatch<React.SetStateAction<PolicyForm>>}){
 const showScheduleConfig=policyForm.composition!=='retention';
 const showRetentionConfig=policyForm.composition!=='schedule';
 return <div className="hbdr-policy-form-fields">
              <div className="hbdr-filter-drawer-body hbdr-policy-drawer-body">
                <section className="hbdr-advanced-filter-section">
                  <h4>Policy Name</h4>
                  <div className="hbdr-advanced-filter-box hbdr-policy-form-box">
                  <input type="text" value={policyForm.name} onChange={event => setPolicyForm({ ...policyForm, name: event.target.value })} className="w-full rounded-xl border border-slate-200 bg-slate-50 px-4 py-2.5 text-sm font-medium outline-none transition-all focus:border-indigo-500 focus:ring-4 focus:ring-indigo-100" placeholder="Example: Core workload-5minute rapid DR" />
                  </div>
                </section>

                <section className="hbdr-advanced-filter-section">
                  <h4>Policy Composition</h4>
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
                <section className="hbdr-advanced-filter-section">
                  <h4>Schedule Type</h4>
                  <p className="hbdr-policy-timezone-note">Schedule times use {userTimeZoneLabel()}.</p>
                  <div className="hbdr-advanced-filter-box hbdr-policy-form-box">
                  <div className="grid grid-cols-2 gap-2">
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
                  <h4>Retention Policy</h4>
                  <div className="hbdr-advanced-filter-box hbdr-policy-form-box">
                  <div className="rounded-xl border border-slate-100 bg-slate-50 p-3">
                    <div className="flex max-w-[260px] items-center gap-3">
                      <input type="number" min={1} value={policyForm.retention} onChange={event => setPolicyForm({ ...policyForm, retention: Number(event.target.value) })} className="w-full rounded-xl border border-slate-200 bg-white px-4 py-2 text-sm font-bold outline-none focus:border-indigo-500" />
                      <span className="text-xs font-bold uppercase text-slate-400">valid copies</span>
                    </div>
                  </div>
                  </div>
                </section>
                )}
              </div>

 </div>;
}
