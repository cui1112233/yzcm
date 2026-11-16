// Minimal JSDOM surface used by the palette test; no runtime or dependency change.
declare module 'jsdom' {
  export class JSDOM {
    constructor(html: string);
    readonly window: { readonly document: Document };
  }
}
