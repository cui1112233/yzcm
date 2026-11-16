import { renderToStaticMarkup } from "react-dom/server";
import { expect, test, vi } from "vitest";
vi.mock("@tarojs/components", () => ({ View: "div", Text: "span", Button: ({ loading, ...props }: any) => <button {...props} />, Input: ({ maxlength, ...props }: any) => <input maxLength={maxlength} {...props} />, Textarea: ({ maxlength, ...props }: any) => <textarea maxLength={maxlength} {...props} />, Image: "img" }));
vi.mock("@tarojs/taro", () => ({ default: {} }));
vi.mock("../../components/AppIcon", () => ({ default: () => null }));
vi.mock("../../config/runtime", () => ({ apiUrl: (path: string) => `https://api.test${path}` }));
import ProfileEditor from "./ProfileEditor";
test("profile page renders the reference avatar and five editable fields with actual member values", () => {
  const html = renderToStaticMarkup(<ProfileEditor center={{ member: { username: "account", displayName: "编导", phone: "123", email: "a@b.cn", teamTitle: "导演", bio: "我的简介" } }} onSaved={() => {}} />);
  for (const label of ["更换头像", "显示名称", "手机号", "邮箱", "团队职务", "个人简介"]) expect(html).toContain(`aria-label="${label}"`);
  expect(html.match(/<input /g)).toHaveLength(4);
  expect(html).toContain("<textarea");
  expect(html).toContain("我的简介</textarea>");
  expect(html).toContain('value="导演"');
  expect(html).toContain("保存个人资料");
  expect(html).not.toContain("未填写");
});
