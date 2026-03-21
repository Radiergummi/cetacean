declare namespace Intl {
  interface DurationFormatOptions {
    style?: "long" | "short" | "narrow" | "digital";
  }

  class DurationFormat {
    constructor(locales?: string | string[], options?: DurationFormatOptions);
    format(duration: {
      hours?: number;
      minutes?: number;
      seconds?: number;
      milliseconds?: number;
    }): string;
  }
}
