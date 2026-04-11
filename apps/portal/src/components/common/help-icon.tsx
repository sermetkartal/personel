import { HelpCircle } from "lucide-react";
import { cn } from "@/lib/utils";

interface HelpIconProps {
  label: string;
  className?: string;
}

export function HelpIcon({ label, className }: HelpIconProps): JSX.Element {
  return (
    <span
      title={label}
      aria-label={label}
      className={cn("inline-flex text-warm-400 hover:text-warm-600 cursor-help", className)}
    >
      <HelpCircle className="w-4 h-4" aria-hidden="true" />
      <span className="sr-only">{label}</span>
    </span>
  );
}
