import { describe, expect, it } from "vitest";
import { backToProject, backToWorks, initialBatchFactoryPageState, openBook, openProject } from "./batchFactoryPageState";

describe("batch factory page state", () => {
  it("opens a batch from personal works", () => {
    expect(openProject(initialBatchFactoryPageState, "batch-7")).toEqual({ mode: "project", selectedBatchId: "batch-7", selectedBookId: "" });
  });

  it("returns from a book to project and then personal works", () => {
    const book = openBook(openProject(initialBatchFactoryPageState, "batch-7"), "book-2");
    const project = backToProject(book);
    expect(project.mode).toBe("project");
    expect(backToWorks(project)).toEqual(initialBatchFactoryPageState);
  });
});
