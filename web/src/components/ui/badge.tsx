import { cva, type VariantProps } from "class-variance-authority";
import { cn } from "../../lib/utils";
import type { HTMLAttributes } from "react";

// フラットな矩形タグ。ピル型（rounded-full）は使わない。
const badgeVariants = cva(
  "inline-flex items-center rounded border px-1.5 py-px text-xs font-medium leading-5",
  {
    variants: {
      variant: {
        default: "border-gray-200 bg-gray-100 text-gray-700",
        blue: "border-blue-200 bg-blue-50 text-blue-800",
        green: "border-green-200 bg-green-50 text-green-800",
        yellow: "border-yellow-300 bg-yellow-50 text-yellow-800",
        red: "border-red-200 bg-red-50 text-red-800",
        slate: "border-slate-200 bg-slate-100 text-slate-700",
      },
    },
    defaultVariants: {
      variant: "default",
    },
  }
);

interface BadgeProps
  extends HTMLAttributes<HTMLSpanElement>,
    VariantProps<typeof badgeVariants> {}

export function Badge({ className, variant, ...props }: BadgeProps) {
  return (
    <span className={cn(badgeVariants({ variant, className }))} {...props} />
  );
}
