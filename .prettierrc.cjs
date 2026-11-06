const { realpathSync } = require("node:fs");
const { createRequire } = require("node:module");

// npx 将 Prettier 与插件放在同一外部缓存环境；从真实 CLI 入口解析，兼容 Windows 包装脚本。
const prettierRequire = createRequire(realpathSync(process.argv[1]));

module.exports = {
  printWidth: 120,
  tabWidth: 2,
  useTabs: false,
  endOfLine: "lf",
  proseWrap: "preserve",
  embeddedLanguageFormatting: "off",
  xmlWhitespaceSensitivity: "strict",
  plugins: [prettierRequire.resolve("@prettier/plugin-xml"), prettierRequire.resolve("prettier-plugin-toml")],
};
