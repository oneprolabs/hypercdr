export type EditFieldProps = {
  label: string;
  value: string;
  onChange: (value: string) => void;
  placeholder?: string;
  type?: string;
  hint?: string;
  required?: boolean;
  disabled?: boolean;
};

export function EditField({ label, value, onChange, placeholder, type = 'text', hint, required, disabled }: EditFieldProps) {
  return (
    <label className="flex flex-col gap-1 text-xs font-semibold tracking-normal text-slate-600">
      <span className="flex items-center gap-1.5">{label}{required && <span className="text-rose-500">*</span>}</span>
      <input
        type={type}
        value={value}
        placeholder={placeholder}
        disabled={disabled}
        onChange={event => onChange(event.target.value)}
        className="h-10 w-full rounded-lg border border-slate-200 bg-white px-3.5 text-sm font-medium text-slate-800 outline-none transition-all placeholder:font-normal placeholder:text-slate-300 hover:border-slate-300 focus:border-blue-500 focus:bg-white focus:shadow-[0_0_0_4px_rgba(59,130,246,0.12)] disabled:cursor-not-allowed disabled:bg-slate-100 disabled:text-slate-500"
      />
      {hint && <span className="text-[10px] font-medium normal-case tracking-normal text-slate-400">{hint}</span>}
    </label>
  );
}
