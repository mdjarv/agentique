export interface Project {
  id: string;
  name: string;
  path: string;
  slug: string;
  default_model: string;
  default_permission_mode: string;
  default_system_prompt: string;
  sort_order: number;
  created_at: string;
  updated_at: string;
}
