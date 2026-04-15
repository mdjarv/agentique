import { File, FileCode, FileText, Folder, Image } from "lucide-react";
import { describe, expect, it } from "vitest";
import {
  formatFileSize,
  getFileIcon,
  getLanguageForSpecialFile,
  getLanguageFromExtension,
  isImageFile,
  isMarkdownFile,
  isPreviewable,
  isTextFile,
} from "~/components/filebrowser/fileUtils";

describe("isImageFile", () => {
  it("matches image extensions", () => {
    expect(isImageFile("photo.png")).toBe(true);
    expect(isImageFile("photo.jpg")).toBe(true);
    expect(isImageFile("photo.svg")).toBe(true);
  });

  it("handles case insensitivity", () => {
    expect(isImageFile("photo.PNG")).toBe(true);
  });

  it("rejects non-image extensions", () => {
    expect(isImageFile("code.ts")).toBe(false);
  });
});

describe("isMarkdownFile", () => {
  it("matches markdown extensions", () => {
    expect(isMarkdownFile("README.md")).toBe(true);
    expect(isMarkdownFile("doc.mdx")).toBe(true);
  });

  it("rejects non-markdown", () => {
    expect(isMarkdownFile("file.txt")).toBe(false);
  });
});

describe("isTextFile", () => {
  it("matches text extensions", () => {
    expect(isTextFile("file.txt")).toBe(true);
  });

  it("matches code extensions", () => {
    expect(isTextFile("file.ts")).toBe(true);
  });

  it("matches markdown", () => {
    expect(isTextFile("file.md")).toBe(true);
  });

  it("rejects images", () => {
    expect(isTextFile("file.png")).toBe(false);
  });
});

describe("getLanguageFromExtension", () => {
  it("returns language for known extensions", () => {
    expect(getLanguageFromExtension("file.ts")).toBe("typescript");
    expect(getLanguageFromExtension("file.go")).toBe("go");
    expect(getLanguageFromExtension("file.py")).toBe("python");
  });

  it("returns undefined for unknown", () => {
    expect(getLanguageFromExtension("file.xyz")).toBeUndefined();
  });
});

describe("getFileIcon", () => {
  it("returns Folder for directories", () => {
    expect(getFileIcon("src", true)).toBe(Folder);
  });

  it("returns Image for image files", () => {
    expect(getFileIcon("photo.png", false)).toBe(Image);
  });

  it("returns FileCode for code files", () => {
    expect(getFileIcon("main.ts", false)).toBe(FileCode);
  });

  it("returns FileText for text files", () => {
    expect(getFileIcon("readme.txt", false)).toBe(FileText);
  });

  it("returns File for unknown", () => {
    expect(getFileIcon("data.xyz", false)).toBe(File);
  });
});

describe("formatFileSize", () => {
  it("formats bytes", () => {
    expect(formatFileSize(500)).toBe("500 B");
  });

  it("formats kilobytes", () => {
    expect(formatFileSize(1024)).toBe("1.0 KB");
  });

  it("formats megabytes", () => {
    expect(formatFileSize(1024 * 1024)).toBe("1.0 MB");
  });

  it("formats gigabytes", () => {
    expect(formatFileSize(1024 * 1024 * 1024)).toBe("1.0 GB");
  });
});

describe("isPreviewable", () => {
  it("returns true for text files", () => {
    expect(isPreviewable("file.ts")).toBe(true);
  });

  it("returns true for images", () => {
    expect(isPreviewable("file.png")).toBe(true);
  });

  it("returns true for special file names", () => {
    expect(isPreviewable("Dockerfile")).toBe(true);
  });

  it("returns false for unknown", () => {
    expect(isPreviewable("file.bin")).toBe(false);
  });
});

describe("getLanguageForSpecialFile", () => {
  it("returns docker for Dockerfile", () => {
    expect(getLanguageForSpecialFile("Dockerfile")).toBe("docker");
  });

  it("returns makefile for Makefile/Justfile", () => {
    expect(getLanguageForSpecialFile("Makefile")).toBe("makefile");
    expect(getLanguageForSpecialFile("Justfile")).toBe("makefile");
    expect(getLanguageForSpecialFile("justfile")).toBe("makefile");
  });

  it("returns undefined for unknown", () => {
    expect(getLanguageForSpecialFile("unknown")).toBeUndefined();
  });
});
