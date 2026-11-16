import { sessionRevision } from "./sessionBoundary";

// Do not continue a multi-request publication under a different login or book.
export async function reviewedPublish<T>(options: {
  create: () => Promise<string>;
  review: (id: string) => Promise<boolean>;
  confirm: (id: string) => Promise<unknown>;
  submit: (id: string) => Promise<T>;
  isCurrent: () => boolean;
}): Promise<T | null> {
  const revision = sessionRevision();
  const check = () => {
    if (revision !== sessionRevision() || !options.isCurrent()) throw new Error("账号或作品已切换，已停止后续发布，请在当前账号重新核对");
  };
  check();
  const id = await options.create();
  check();
  if (!id) throw new Error("公网没有返回发布确认单编号");
  if (!await options.review(id)) return null;
  check();
  await options.confirm(id);
  check();
  const receipt = await options.submit(id);
  check();
  return receipt;
}
