import { beforeEach, expect, test, vi } from "vitest";
const state=vi.hoisted(()=>({storage:new Map<string,any>(),origin:"https://yzcm.site"}));
const request=vi.hoisted(()=>vi.fn());
vi.mock("@tarojs/taro",()=>({default:{request,getStorageSync:(key:string)=>state.storage.get(key),setStorageSync:(key:string,value:any)=>state.storage.set(key,value),removeStorageSync:(key:string)=>state.storage.delete(key)}}));
vi.mock("../config/runtime",()=>({requireApiOrigin:()=>{},apiUrl:(path:string)=>new URL(path,state.origin).toString()}));
beforeEach(()=>{vi.resetModules();request.mockReset();state.storage.clear();state.origin="https://yzcm.site";});
function loginResponses(){request.mockResolvedValueOnce({statusCode:200,cookies:["ycm_session=receipt; Path=/; HttpOnly; Secure; Max-Age=3600"],data:{token:"verified"}}).mockResolvedValueOnce({statusCode:200,data:{username:"alice"}});}
test("unremembered login works in this process but publishes no durable credentials",async()=>{
  const session=await import("./session");loginResponses();
  await session.loginAccount("alice","never-store-this","",{remember:false});
  expect(session.hasAccountSession()).toBe(true);
  expect(request.mock.calls[0][0].data.remember).toBe(false);
  expect(JSON.stringify([...state.storage])).not.toMatch(/never-store-this|verified|receipt/);
  vi.resetModules();expect((await import("./session")).hasAccountSession()).toBe(false);
});
test("remembered Cookie survives restart, is origin scoped, expires, and is removed on logout",async()=>{
  const session=await import("./session");loginResponses();
  await session.loginAccount("alice","never-store-this","",{remember:true});
  expect(JSON.stringify([...state.storage])).not.toContain("never-store-this");
  vi.resetModules();const restored=await import("./session");
  expect(restored.getSessionCookie()).toBe("ycm_session=receipt");
  state.origin="https://other.example";expect(restored.getSessionCookie()).toBe("");
  state.origin="https://yzcm.site";
  request.mockResolvedValueOnce({statusCode:204});await restored.logoutAccount();
  vi.resetModules();expect((await import("./session")).hasAccountSession()).toBe(false);
});
test("expired remembered Cookie is never attached after restart",async()=>{
  const session=await import("./session");loginResponses();
  await session.loginAccount("alice","password","",{remember:true});
  const now=vi.spyOn(Date,"now").mockReturnValue(Date.now()+3600_001);
  try{vi.resetModules();expect((await import("./session")).getSessionCookie()).toBe("");}finally{now.mockRestore();}
});
