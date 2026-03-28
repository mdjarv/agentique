import { createFileRoute, useNavigate } from "@tanstack/react-router";
import { useCallback, useState } from "react";
import { z } from "zod";
import { FileBreadcrumbs } from "~/components/filebrowser/FileBreadcrumbs";
import { FileList } from "~/components/filebrowser/FileList";
import { FilePreview } from "~/components/filebrowser/FilePreview";
import { PageHeader } from "~/components/layout/PageHeader";
import { useIsMobile } from "~/hooks/useIsMobile";
import { useAppStore } from "~/stores/app-store";

const searchSchema = z.object({
  path: z.string().optional().default(""),
  file: z.string().optional(),
});

export const Route = createFileRoute("/project/$projectSlug/files")({
  component: FileBrowserPage,
  validateSearch: searchSchema,
});

function FileBrowserPage() {
  const { projectSlug } = Route.useParams();
  const { path, file } = Route.useSearch();
  const navigate = useNavigate();
  const isMobile = useIsMobile();
  const project = useAppStore((s) => s.projects.find((p) => p.slug === projectSlug));

  // Local hover/select state for non-URL-driven interactions.
  const [selectedFile, setSelectedFile] = useState<string | null>(file ?? null);

  const handleNavigateDir = useCallback(
    (dirPath: string) => {
      setSelectedFile(null);
      navigate({
        to: "/project/$projectSlug/files",
        params: { projectSlug },
        search: { path: dirPath || undefined },
      });
    },
    [navigate, projectSlug],
  );

  const handleSelectFile = useCallback(
    (filePath: string) => {
      setSelectedFile(filePath);
      navigate({
        to: "/project/$projectSlug/files",
        params: { projectSlug },
        search: { path, file: filePath },
        replace: true,
      });
    },
    [navigate, projectSlug, path],
  );

  const handleClosePreview = useCallback(() => {
    setSelectedFile(null);
    navigate({
      to: "/project/$projectSlug/files",
      params: { projectSlug },
      search: { path },
      replace: true,
    });
  }, [navigate, projectSlug, path]);

  if (!project) return null;

  // Mobile: show preview OR list, not both.
  if (isMobile && selectedFile) {
    return (
      <div className="flex flex-col h-full">
        <FilePreview projectId={project.id} filePath={selectedFile} onClose={handleClosePreview} />
      </div>
    );
  }

  return (
    <div className="flex flex-col h-full">
      <PageHeader>
        <span className="font-semibold truncate">{project.name}</span>
        <span className="text-muted-foreground mx-1">/</span>
        <span className="text-muted-foreground">Files</span>
      </PageHeader>

      <FileBreadcrumbs projectName={project.name} path={path} onNavigate={handleNavigateDir} />

      <div className="flex flex-1 min-h-0">
        {/* File list */}
        <div className={`flex flex-col ${selectedFile && !isMobile ? "w-1/2 border-r" : "w-full"}`}>
          <FileList
            projectId={project.id}
            path={path}
            selectedFile={selectedFile}
            onNavigate={handleNavigateDir}
            onSelectFile={handleSelectFile}
          />
        </div>

        {/* Preview panel (desktop only) */}
        {selectedFile && !isMobile && (
          <div className="w-1/2 flex flex-col min-h-0">
            <FilePreview
              projectId={project.id}
              filePath={selectedFile}
              onClose={handleClosePreview}
            />
          </div>
        )}
      </div>
    </div>
  );
}
