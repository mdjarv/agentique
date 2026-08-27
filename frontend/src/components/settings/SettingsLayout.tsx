/**
 * The settings shell: a category rail on the left, one panel on the right.
 *
 * Settings live at their own route rather than in the sidebar footer's
 * popover, which had accumulated usage, machines, the Claude account, theme
 * and sign-out in one 288px column. The rail is the index; each category owns
 * its URL so a link can point at one.
 */
import { Link, Outlet, useMatchRoute } from "@tanstack/react-router";
import {
  AudioLines,
  FileText,
  FolderGit2,
  Info,
  type LucideIcon,
  Palette,
  Server,
  UserCircle,
} from "lucide-react";
import { PageHeader } from "~/components/layout/PageHeader";
import { cn } from "~/lib/utils";

interface Category {
  to: string;
  label: string;
  icon: LucideIcon;
  /** One line under the panel title — what this category is for. */
  blurb: string;
}

// Ordered registry -> content -> presentation -> identity. Projects and
// Machines lead because they are the same kind of thing: the repos you work in
// and the hosts you work across, each registered once and given a face.
export const SETTINGS_CATEGORIES: Category[] = [
  {
    to: "/settings/projects",
    label: "Projects",
    icon: FolderGit2,
    blurb: "The repos agentique can open a session in.",
  },
  {
    to: "/settings/machines",
    label: "Machines",
    icon: Server,
    blurb: "Name the machines you work across and give them a face.",
  },
  {
    to: "/settings/templates",
    label: "Templates",
    icon: FileText,
    blurb: "Reusable prompts, with their settings, for any project.",
  },
  {
    to: "/settings/appearance",
    label: "Appearance",
    icon: Palette,
    blurb: "How agentique looks on this device.",
  },
  {
    to: "/settings/voice",
    label: "Voice",
    icon: AudioLines,
    blurb: "How the live voice agent sounds and how much it says.",
  },
  {
    to: "/settings/account",
    label: "Account",
    icon: UserCircle,
    blurb: "The Claude account sessions run as, and your sign-in here.",
  },
  { to: "/settings/about", label: "About", icon: Info, blurb: "This build and what it has on." },
];

export function SettingsLayout() {
  const matchRoute = useMatchRoute();
  const active = SETTINGS_CATEGORIES.find((c) => matchRoute({ to: c.to, fuzzy: false }));

  return (
    <div className="flex h-full flex-col">
      <PageHeader>
        <span className="flex min-w-0 items-baseline gap-2">
          <span className="shrink-0 text-muted-foreground">Settings</span>
          {active && (
            <>
              <span className="shrink-0 text-muted-foreground-faint">/</span>
              <span className="truncate font-semibold text-foreground-bright">{active.label}</span>
            </>
          )}
        </span>
      </PageHeader>

      <div className="flex min-h-0 flex-1">
        <nav className="flex w-52 shrink-0 flex-col gap-0.5 border-r border-border/60 p-2 max-md:w-14">
          {SETTINGS_CATEGORIES.map(({ to, label, icon: Icon }) => (
            <Link
              key={to}
              to={to}
              title={label}
              className={cn(
                "flex items-center gap-2.5 rounded-md px-2.5 py-2 text-[13px] transition-colors max-md:justify-center max-md:px-0",
                "text-muted-foreground hover:bg-muted/50 hover:text-foreground",
              )}
              activeProps={{ className: "bg-secondary text-foreground-bright font-medium" }}
            >
              <Icon className="size-4 shrink-0" />
              <span className="truncate max-md:hidden">{label}</span>
            </Link>
          ))}
        </nav>

        <div className="min-w-0 flex-1 overflow-y-auto">
          {/* 3xl rather than 2xl: a project row carries a line per machine
              holding a checkout, and those cramp badly in a narrower column. */}
          <div className="mx-auto flex max-w-3xl flex-col gap-6 px-6 py-6 max-md:px-4">
            {active && (
              <div className="flex flex-col gap-1">
                <h1 className="text-lg font-semibold text-foreground-bright">{active.label}</h1>
                <p className="text-[13px] text-muted-foreground">{active.blurb}</p>
              </div>
            )}
            <Outlet />
          </div>
        </div>
      </div>
    </div>
  );
}

/** A titled block inside a settings panel. */
export function SettingsSection({
  title,
  description,
  action,
  children,
}: {
  title: string;
  description?: string;
  action?: React.ReactNode;
  children: React.ReactNode;
}) {
  return (
    <section className="flex flex-col gap-3">
      <div className="flex items-baseline gap-3">
        <div className="flex min-w-0 flex-col gap-0.5">
          <h2 className="text-[13px] font-semibold uppercase tracking-wider text-muted-foreground">
            {title}
          </h2>
          {description && (
            <p className="text-[12.5px] text-muted-foreground-faint">{description}</p>
          )}
        </div>
        {action && <div className="ml-auto shrink-0">{action}</div>}
      </div>
      {children}
    </section>
  );
}

/** One labelled control line — label + description left, control right. */
export function SettingsRow({
  label,
  description,
  control,
}: {
  label: string;
  // ReactNode rather than string: some rows carry a line that must stand out
  // from the rest of the description (a warning, say) rather than being
  // concatenated into the same grey run of text.
  description?: React.ReactNode;
  control: React.ReactNode;
}) {
  return (
    <div className="flex items-center gap-4 rounded-lg border border-border/60 bg-card px-3.5 py-3">
      <div className="flex min-w-0 flex-col gap-0.5">
        <span className="text-[13px] font-medium text-foreground">{label}</span>
        {description && (
          <span className="text-[12px] text-muted-foreground-faint">{description}</span>
        )}
      </div>
      <div className="ml-auto shrink-0">{control}</div>
    </div>
  );
}
