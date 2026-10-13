import React, { useEffect, useRef, useState } from 'react';
import { Check, ChevronDown } from 'lucide-react';

/** In-page select avoids the OS popup's light selection flash. */
export function RecoverySelect({children,value,onChange,disabled,...props}: React.SelectHTMLAttributes<HTMLSelectElement>) {
 const [open,setOpen]=useState(false);const root=useRef<HTMLDivElement>(null);const trigger=useRef<HTMLButtonElement>(null);
 const options=React.Children.toArray(children).filter(React.isValidElement) as React.ReactElement<React.OptionHTMLAttributes<HTMLOptionElement>>[];
 const selected=options.find(o=>String(o.props.value)===String(value));
 useEffect(()=>{if(!open)return;const close=(e:PointerEvent)=>{if(!root.current?.contains(e.target as Node))setOpen(false)};document.addEventListener('pointerdown',close);root.current?.querySelector<HTMLButtonElement>('[role=option][aria-selected=true]:not(:disabled)')?.focus();return()=>document.removeEventListener('pointerdown',close)},[open]);
 return <div className="hbdr-recovery-select" ref={root} onKeyDown={e=>{if(e.key==='Escape'){e.stopPropagation();setOpen(false);trigger.current?.focus()}if(open&&['ArrowDown','ArrowUp','Home','End'].includes(e.key)){e.preventDefault();const buttons=[...root.current!.querySelectorAll<HTMLButtonElement>('[role=option]:not(:disabled)')];const i=buttons.indexOf(document.activeElement as HTMLButtonElement);buttons[e.key==='Home'?0:e.key==='End'?buttons.length-1:(i+(e.key==='ArrowDown'?1:-1)+buttons.length)%buttons.length]?.focus()}}}>
 <button ref={trigger} type="button" role="combobox" aria-label={props['aria-label']||'Select option'} aria-describedby={props['aria-describedby']} aria-expanded={open} aria-haspopup="listbox" disabled={disabled} onClick={()=>setOpen(!open)} onKeyDown={e=>{if(['ArrowDown','ArrowUp'].includes(e.key)){e.preventDefault();setOpen(true)}}}>{selected?.props.children}<ChevronDown size={14}/></button>
 {open&&<div role="listbox">{options.map((o,i)=><button key={i} type="button" role="option" aria-selected={String(o.props.value)===String(value)} disabled={o.props.disabled} onClick={()=>{onChange?.({target:{value:String(o.props.value)}} as React.ChangeEvent<HTMLSelectElement>);setOpen(false);trigger.current?.focus()}}><span>{o.props.children}</span>{String(o.props.value)===String(value)&&<Check size={14}/>}</button>)}</div>}
 </div>;
}
