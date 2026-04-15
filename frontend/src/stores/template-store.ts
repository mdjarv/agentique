import { create } from "zustand";
import type { PromptTemplate } from "~/lib/generated-types";
import {
  type CreateTemplateInput,
  createTemplate,
  deleteTemplate,
  listTemplates,
  type UpdateTemplateInput,
  updateTemplate,
} from "~/lib/template-api";

interface TemplateState {
  templates: PromptTemplate[];
  loaded: boolean;
  loading: boolean;

  load: () => Promise<void>;
  create: (input: CreateTemplateInput) => Promise<PromptTemplate>;
  update: (id: string, input: UpdateTemplateInput) => Promise<PromptTemplate>;
  remove: (id: string) => Promise<void>;
}

export const useTemplateStore = create<TemplateState>((set, get) => ({
  templates: [],
  loaded: false,
  loading: false,

  load: async () => {
    if (get().loading) return;
    set({ loading: true });
    try {
      const templates = await listTemplates();
      set({ templates, loaded: true, loading: false });
    } catch (err) {
      console.error("Failed to load templates:", err);
      set({ loading: false });
    }
  },

  create: async (input) => {
    const tmpl = await createTemplate(input);
    set((s) => ({ templates: [...s.templates, tmpl] }));
    return tmpl;
  },

  update: async (id, input) => {
    const tmpl = await updateTemplate(id, input);
    set((s) => ({ templates: s.templates.map((t) => (t.id === id ? tmpl : t)) }));
    return tmpl;
  },

  remove: async (id) => {
    await deleteTemplate(id);
    set((s) => ({ templates: s.templates.filter((t) => t.id !== id) }));
  },
}));
