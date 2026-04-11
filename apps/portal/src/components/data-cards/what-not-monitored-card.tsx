import type { LucideIcon } from "lucide-react";
import { CheckCircle2 } from "lucide-react";

interface WhatNotMonitoredCardProps {
  icon: LucideIcon;
  title: string;
  description: string;
}

/**
 * Card for the "Neler İzlenmiyor?" trust-building page.
 * Uses green trust palette to signal openness and honesty.
 */
export function WhatNotMonitoredCard({
  icon: Icon,
  title,
  description,
}: WhatNotMonitoredCardProps): JSX.Element {
  return (
    <article className="card group hover:border-trust-200 transition-colors duration-150">
      <div className="flex items-start gap-4">
        <div
          className="relative w-10 h-10 rounded-xl bg-trust-50 flex items-center justify-center flex-shrink-0"
          aria-hidden="true"
        >
          <Icon className="w-5 h-5 text-trust-600" />
          <CheckCircle2 className="absolute -bottom-1 -right-1 w-4 h-4 text-trust-500 bg-white rounded-full" />
        </div>

        <div className="flex-1 min-w-0">
          <h3 className="font-medium text-warm-900 leading-snug">{title}</h3>
          <p className="mt-2 text-sm text-warm-600 leading-relaxed">
            {description}
          </p>
        </div>
      </div>
    </article>
  );
}
