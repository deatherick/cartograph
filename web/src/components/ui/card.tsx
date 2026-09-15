// Base shape adapted from Grafel's webui-v2 (MIT License) — see
// NOTICE.md. CardTag/CardKicker are this project's own addition, part
// of the Observatory-inspired redesign (docs/adr — the mono-tag-above-
// heading pattern used throughout that field guide's own component
// system, ported here as a small reusable primitive rather than
// hand-repeated per page).
import { cn } from "@/lib/utils";

export function Card({ className, ...props }: React.HTMLAttributes<HTMLDivElement>) {
  return (
    <div
      className={cn(
        "rounded-lg bg-surface border border-border shadow-[var(--shadow-1)]",
        className,
      )}
      {...props}
    />
  );
}

export function CardHeader({ className, ...props }: React.HTMLAttributes<HTMLDivElement>) {
  return <div className={cn("p-4 border-b border-border-soft", className)} {...props} />;
}

export function CardTitle({ className, ...props }: React.HTMLAttributes<HTMLHeadingElement>) {
  return <h3 className={cn("text-lg font-semibold text-text", className)} {...props} />;
}

export function CardBody({ className, ...props }: React.HTMLAttributes<HTMLDivElement>) {
  return <div className={cn("p-4", className)} {...props} />;
}

/** Small uppercase mono chip sitting above a card's own title — "what
 * kind of thing is this card", read before the title itself. */
export function CardTag({ className, ...props }: React.HTMLAttributes<HTMLSpanElement>) {
  return (
    <span
      className={cn(
        "inline-block font-mono text-[10.5px] text-accent bg-accent-soft",
        "px-1.5 py-0.5 rounded mb-1.5 leading-none",
        className,
      )}
      {...props}
    />
  );
}
