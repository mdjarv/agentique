import { createFileRoute, useNavigate } from "@tanstack/react-router";
import { ArrowLeft } from "lucide-react";
import { PageHeader } from "~/components/layout/PageHeader";
import { ProfileForm } from "~/components/team/ProfileForm";
import { Button } from "~/components/ui/button";

export const Route = createFileRoute("/teams_/personas/new")({
  component: NewProfilePage,
});

function NewProfilePage() {
  const navigate = useNavigate();
  return (
    <div className="flex flex-col h-full">
      <PageHeader>
        <Button
          variant="ghost"
          size="icon"
          className="-ml-1 size-7"
          onClick={() => navigate({ to: "/teams" })}
          aria-label="Back to Teams"
        >
          <ArrowLeft className="size-4" />
        </Button>
        <span className="font-semibold">New Agent Profile</span>
      </PageHeader>
      <div className="flex-1 overflow-y-auto">
        <ProfileForm
          onSaved={() => navigate({ to: "/teams" })}
          onCancel={() => navigate({ to: "/teams" })}
        />
      </div>
    </div>
  );
}
