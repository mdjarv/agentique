import { FileText, Search, Variable } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { Badge } from "~/components/ui/badge";
import { Popover, PopoverContent, PopoverTrigger } from "~/components/ui/popover";
import { Tooltip, TooltipContent, TooltipTrigger } from "~/components/ui/tooltip";
import type { PromptTemplate } from "~/lib/generated-types";
import { extractVariables, parseTags } from "~/lib/template-utils";
import { cn } from "~/lib/utils";
import { useTemplateStore } from "~/stores/template-store";

interface TemplatePickerProps {
  onSelect: (template: PromptTemplate) => void;
  disabled?: boolean;
}

export function TemplatePicker({ onSelect, disabled }: TemplatePickerProps) {
  const { templates, loaded, load } = useTemplateStore();
  const [open, setOpen] = useState(false);
  const [search, setSearch] = useState("");

  useEffect(() => {
    if (open && !loaded) load();
  }, [open, loaded, load]);

  const filtered = useMemo(() => {
    if (!search) return templates;
    const q = search.toLowerCase();
    return templates.filter((t) => {
      const tags = parseTags(t.tags);
      return (
        t.name.toLowerCase().includes(q) ||
        t.description.toLowerCase().includes(q) ||
        tags.some((tag) => tag.includes(q))
      );
    });
  }, [templates, search]);

  const handleSelect = (tmpl: PromptTemplate) => {
    setOpen(false);
    setSearch("");
    onSelect(tmpl);
  };

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <Tooltip>
        <TooltipTrigger asChild>
          <PopoverTrigger asChild>
            <button
              type="button"
              disabled={disabled}
              className="h-7 w-7 max-md:h-10 max-md:w-10 rounded-lg text-muted-foreground hover:text-foreground hover:bg-muted/80 flex items-center justify-center transition-colors disabled:opacity-40 cursor-pointer"
              aria-label="Use template"
            >
              <FileText className="h-3.5 w-3.5" />
            </button>
          </PopoverTrigger>
        </TooltipTrigger>
        <TooltipContent side="top">Use a template</TooltipContent>
      </Tooltip>
      <PopoverContent className="w-80 p-0" align="start" side="top" sideOffset={8}>
        <div className="p-2 border-b">
          <div className="flex items-center gap-2 px-2">
            <Search className="h-3.5 w-3.5 text-muted-foreground shrink-0" />
            <input
              value={search}
              onChange={(e) => setSearch(e.target.value)}
              placeholder="Search templates..."
              className="flex-1 bg-transparent text-sm outline-none placeholder:text-muted-foreground"
              autoFocus
            />
          </div>
        </div>
        <div className="max-h-64 overflow-y-auto p-1">
          {filtered.length === 0 && (
            <div className="px-3 py-6 text-center text-sm text-muted-foreground">
              {templates.length === 0 ? "No templates yet" : "No matches"}
            </div>
          )}
          {filtered.map((tmpl) => {
            const tags = parseTags(tmpl.tags);
            const vars = extractVariables(tmpl.content);
            return (
              <button
                key={tmpl.id}
                type="button"
                onClick={() => handleSelect(tmpl)}
                className={cn(
                  "w-full text-left rounded-md px-3 py-2 hover:bg-muted/60 transition-colors flex items-start gap-2.5",
                )}
              >
                <FileText className="h-4 w-4 text-muted-foreground mt-0.5 shrink-0" />
                <div className="flex-1 min-w-0">
                  <div className="flex items-center gap-1.5">
                    <span className="text-sm font-medium truncate">{tmpl.name}</span>
                    {vars.length > 0 && (
                      <span className="flex items-center gap-0.5 text-[10px] text-muted-foreground">
                        <Variable className="h-2.5 w-2.5" />
                        {vars.length}
                      </span>
                    )}
                  </div>
                  {tmpl.description && (
                    <p className="text-xs text-muted-foreground truncate">{tmpl.description}</p>
                  )}
                  {tags.length > 0 && (
                    <div className="flex gap-1 mt-1 flex-wrap">
                      {tags.map((tag) => (
                        <Badge
                          key={tag}
                          variant="secondary"
                          className="text-[9px] px-1 py-0 leading-tight"
                        >
                          {tag}
                        </Badge>
                      ))}
                    </div>
                  )}
                </div>
              </button>
            );
          })}
        </div>
      </PopoverContent>
    </Popover>
  );
}
