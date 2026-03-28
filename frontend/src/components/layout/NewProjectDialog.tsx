import { useNavigate } from "@tanstack/react-router";
import { FolderOpen, Plus } from "lucide-react";
import { useCallback, useState } from "react";
import { DirectoryBrowser } from "~/components/layout/DirectoryBrowser";
import { Button } from "~/components/ui/button";
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "~/components/ui/dialog";
import { Input } from "~/components/ui/input";
import { Label } from "~/components/ui/label";
import { Tooltip, TooltipContent, TooltipTrigger } from "~/components/ui/tooltip";
import { useIsMobile } from "~/hooks/useIsMobile";
import { createProject } from "~/lib/api";
import { useAppStore } from "~/stores/app-store";

/** Extract the last directory component from a path (cross-platform). */
function dirName(p: string): string {
  const trimmed = p.replace(/[/\\]+$/, "");
  const parts = trimmed.split(/[/\\]/);
  return parts[parts.length - 1] ?? "";
}

export function NewProjectDialog() {
  const [open, setOpen] = useState(false);
  const [name, setName] = useState("");
  const [nameManuallySet, setNameManuallySet] = useState(false);
  const [path, setPath] = useState("");
  const [error, setError] = useState("");
  const [showBrowser, setShowBrowser] = useState(true);
  const addProject = useAppStore((s) => s.addProject);
  const navigate = useNavigate();
  const isMobile = useIsMobile();

  const handlePathChange = useCallback(
    (newPath: string) => {
      setPath(newPath);
      setError("");
      if (!nameManuallySet) {
        setName(dirName(newPath));
      }
    },
    [nameManuallySet],
  );

  const handleNameChange = (newName: string) => {
    setName(newName);
    setNameManuallySet(newName !== "");
  };

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setError("");
    try {
      const project = await createProject(name, path);
      addProject(project);
      setName("");
      setPath("");
      setNameManuallySet(false);
      setOpen(false);
      navigate({
        to: "/project/$projectSlug",
        params: { projectSlug: project.slug },
      });
    } catch {
      setError("Failed to create project. Check that the path exists.");
    }
  };

  const handleOpenChange = (isOpen: boolean) => {
    setOpen(isOpen);
    if (!isOpen) {
      setName("");
      setPath("");
      setNameManuallySet(false);
      setError("");
      setShowBrowser(true);
    }
  };

  const canCreate = path.trim() !== "";

  return (
    <Dialog open={open} onOpenChange={handleOpenChange}>
      {isMobile ? (
        <DialogTrigger asChild>
          <Button variant="ghost" size="icon-sm" aria-label="New project">
            <Plus className="h-4 w-4" />
          </Button>
        </DialogTrigger>
      ) : (
        <Tooltip>
          <TooltipTrigger asChild>
            <DialogTrigger asChild>
              <Button variant="ghost" size="icon-sm" aria-label="New project">
                <Plus className="h-4 w-4" />
              </Button>
            </DialogTrigger>
          </TooltipTrigger>
          <TooltipContent>New project</TooltipContent>
        </Tooltip>
      )}
      <DialogContent className={showBrowser ? "sm:max-w-2xl" : undefined}>
        <DialogHeader>
          <DialogTitle>Create New Project</DialogTitle>
        </DialogHeader>
        <form onSubmit={handleSubmit} className="space-y-4">
          <div className="space-y-2">
            <Label htmlFor="project-path">Directory</Label>
            <div className="flex gap-2">
              <Input
                id="project-path"
                value={path}
                onChange={(e) => handlePathChange(e.target.value)}
                placeholder="/home/user/my-project"
                className="flex-1"
              />
              <Button
                type="button"
                variant="outline"
                size="icon"
                onClick={() => setShowBrowser(!showBrowser)}
                title={showBrowser ? "Hide browser" : "Browse"}
              >
                <FolderOpen className="h-4 w-4" />
              </Button>
            </div>
          </div>
          {showBrowser && <DirectoryBrowser onSelect={handlePathChange} />}
          <div className="space-y-2">
            <Label htmlFor="project-name">Name</Label>
            <Input
              id="project-name"
              value={name}
              onChange={(e) => handleNameChange(e.target.value)}
              placeholder="Auto-filled from directory"
            />
          </div>
          {error && <p className="text-sm text-destructive">{error}</p>}
          <DialogFooter>
            <DialogClose asChild>
              <Button type="button" variant="outline">
                Cancel
              </Button>
            </DialogClose>
            <Button type="submit" disabled={!canCreate}>
              Create
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
