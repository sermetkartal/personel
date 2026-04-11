import { Clock } from "lucide-react";

interface RetentionCardProps {
  category: string;
  period: string;
  note?: string;
}

export function RetentionCard({
  category,
  period,
  note,
}: RetentionCardProps): JSX.Element {
  return (
    <div className="flex items-center gap-3 py-3 border-b border-warm-100 last:border-0">
      <Clock className="w-4 h-4 text-warm-300 flex-shrink-0" aria-hidden="true" />
      <div className="flex-1 min-w-0">
        <span className="text-sm text-warm-800 font-medium">{category}</span>
        {note && (
          <span className="block text-xs text-warm-400 mt-0.5">{note}</span>
        )}
      </div>
      <span className="text-sm text-portal-700 font-medium flex-shrink-0 tabular-nums">
        {period}
      </span>
    </div>
  );
}
