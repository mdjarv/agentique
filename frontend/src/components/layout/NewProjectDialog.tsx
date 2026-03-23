import { useNavigate } from "@tanstack/react-router";
import { Plus } from "lucide-react";
import { useState } from "react";
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
	const addProject = useAppStore((s) => s.addProject);
	const navigate = useNavigate();

	const handlePathChange = (newPath: string) => {
		setPath(newPath);
		setError("");
		if (!nameManuallySet) {
			setName(dirName(newPath));
		}
	};

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
				to: "/project/$projectId",
				params: { projectId: project.id },
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
		}
	};

	const canCreate = path.trim() !== "";

	return (
		<Dialog open={open} onOpenChange={handleOpenChange}>
			<DialogTrigger asChild>
				<Button variant="outline" className="w-full gap-2">
					<Plus className="h-4 w-4" />
					New Project
				</Button>
			</DialogTrigger>
			<DialogContent>
				<DialogHeader>
					<DialogTitle>Create New Project</DialogTitle>
				</DialogHeader>
				<form onSubmit={handleSubmit} className="space-y-4">
					<div className="space-y-2">
						<Label htmlFor="project-path">Directory</Label>
						<Input
							id="project-path"
							value={path}
							onChange={(e) => handlePathChange(e.target.value)}
							placeholder="/home/user/my-project"
						/>
					</div>
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
