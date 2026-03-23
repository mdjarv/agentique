import { NewProjectDialog } from "~/components/layout/NewProjectDialog";
import { ProjectList } from "~/components/layout/ProjectList";

export function AppSidebar() {
	return (
		<div className="w-64 border-r bg-muted/30 flex flex-col h-full">
			<div className="p-4 font-semibold text-lg border-b">Agentique</div>
			<div className="flex-1 overflow-y-auto">
				<ProjectList />
			</div>
			<div className="p-3 border-t">
				<NewProjectDialog />
			</div>
		</div>
	);
}
