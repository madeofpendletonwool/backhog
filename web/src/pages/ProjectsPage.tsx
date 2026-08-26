import { ListChecks, Plus, Sparkles, Target } from "lucide-react";
import { useState } from "react";
import { Link } from "react-router-dom";

import { CreateProjectDialog } from "@/components/CreateProjectDialog";
import { ProgressBar } from "@/components/ProgressBar";
import { Button, EmptyState, Skeleton } from "@/components/ui/primitives";
import { useProjects } from "@/hooks/useProjects";
import { formatDate, formatHours } from "@/lib/format";
import { PROJECT_KIND_LABELS, type Project, type ProjectKind } from "@/lib/types";

const KIND_ICONS: Record<ProjectKind, React.ReactNode> = {
  checklist: <ListChecks className="size-3.5" />,
  count_goal: <Target className="size-3.5" />,
  rule_goal: <Sparkles className="size-3.5" />,
};

export function ProjectsPage() {
  const { data, isLoading } = useProjects();
  const [creating, setCreating] = useState(false);

  const projects = data?.projects ?? [];
  const active = projects.filter((project) => !project.completed_at);
  const completed = projects.filter((project) => project.completed_at);

  return (
    <div className="mx-auto max-w-5xl px-4 py-6 sm:px-6 lg:px-8 lg:py-8">
      <header className="mb-6 flex items-start justify-between gap-4">
        <div>
          <h1 className="text-2xl font-semibold tracking-tight text-ink-100">Projects</h1>
          <p className="mt-1 text-sm text-ink-400">
            Temporary objectives — finish a set, hit a count, clear a slice of the backlog.
          </p>
        </div>
        <Button variant="primary" onClick={() => setCreating(true)}>
          <Plus className="size-4" />
          New project
        </Button>
      </header>

      {isLoading ? (
        <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
          {Array.from({ length: 4 }).map((_, index) => (
            <Skeleton key={index} className="h-28" />
          ))}
        </div>
      ) : projects.length === 0 ? (
        <EmptyState
          icon={<Target className="size-7" />}
          title="No projects yet"
          description="Lists are what exists. Projects are what you're trying to accomplish — give one a target and go."
          action={
            <Button variant="primary" onClick={() => setCreating(true)}>
              Create a project
            </Button>
          }
        />
      ) : (
        <div className="space-y-8">
          {active.length > 0 && (
            <Section title="In progress" caption="Still working toward the target." projects={active} />
          )}
          {completed.length > 0 && (
            <Section
              title="Completed"
              caption="Targets met — or closed by hand. Enjoy the W."
              projects={completed}
            />
          )}
        </div>
      )}

      <CreateProjectDialog open={creating} onClose={() => setCreating(false)} />
    </div>
  );
}

function Section({
  title,
  caption,
  projects,
}: {
  title: string;
  caption: string;
  projects: Project[];
}) {
  return (
    <section>
      <div className="mb-3">
        <h2 className="text-sm font-semibold text-ink-200">{title}</h2>
        <p className="text-xs text-ink-500">{caption}</p>
      </div>
      <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
        {projects.map((project) => (
          <ProjectCard key={project.id} project={project} />
        ))}
      </div>
    </section>
  );
}

function ProjectCard({ project }: { project: Project }) {
  const { progress } = project;
  const complete = Boolean(project.completed_at);

  return (
    <Link
      to={`/projects/${project.id}`}
      className="panel group flex flex-col p-4 transition-all duration-200 hover:-translate-y-0.5 hover:border-white/[0.14] focus-visible:focus-ring"
    >
      <div className="flex items-start justify-between gap-3">
        <div className="min-w-0">
          <p className="flex items-center gap-1.5 truncate font-medium text-ink-100">
            {KIND_ICONS[project.kind]}
            {project.name}
          </p>
          {project.description && (
            <p className="mt-1 line-clamp-1 text-xs leading-relaxed text-ink-500">
              {project.description}
            </p>
          )}
        </div>
        <span className="shrink-0 rounded-lg bg-white/[0.06] px-2 py-1 text-[11px] font-medium text-ink-400">
          {PROJECT_KIND_LABELS[project.kind]}
        </span>
      </div>

      <div className="mt-auto pt-4">
        <ProgressBar percent={progress.percent} complete={complete} />
        <div className="mt-2 flex items-center justify-between gap-2 text-xs">
          <span className="tabular-nums text-ink-300">
            {progress.completed_count}/{progress.target_count} finished
          </span>
          <span className="tabular-nums text-ink-500">
            {complete
              ? `Done ${formatDate(project.completed_at ?? null)}`
              : `${formatHours(progress.est_hours_remaining)} to go`}
          </span>
        </div>
      </div>
    </Link>
  );
}
