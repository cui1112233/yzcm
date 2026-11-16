import { sessionRevision } from "./sessionBoundary";

export async function sessionConfirmation<T extends { confirm: boolean }>(review: () => Promise<T>): Promise<T> {
  const revision = sessionRevision();
  const result = await review();
  return revision === sessionRevision() ? result : { ...result, confirm: false };
}
