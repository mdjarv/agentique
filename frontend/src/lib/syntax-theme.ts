import { oneDark, oneLight } from "react-syntax-highlighter/dist/esm/styles/prism";

export function getSyntaxTheme(resolvedTheme: "light" | "dark") {
  return resolvedTheme === "dark" ? oneDark : oneLight;
}
