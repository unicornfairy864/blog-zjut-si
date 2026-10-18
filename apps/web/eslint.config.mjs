import { dirname } from "path";
import { fileURLToPath } from "url";
import { FlatCompat } from "@eslint/eslintrc";
import jsxA11y from "eslint-plugin-jsx-a11y";

const __filename = fileURLToPath(import.meta.url);
const __dirname = dirname(__filename);

const compat = new FlatCompat({
  baseDirectory: __dirname,
});

// 可访问性规则强制开启（agent.md §8：eslint-plugin-jsx-a11y）。
const eslintConfig = [
  ...compat.extends("next/core-web-vitals", "next/typescript"),
  {
    plugins: { "jsx-a11y": jsxA11y },
    rules: { ...jsxA11y.configs.recommended.rules },
  },
  {
    // shadcn 生成物不手改：这些组件把 children 透传，静态分析看不出标题内容。
    files: ["ui/shadcn/**/*.tsx"],
    rules: {
      "jsx-a11y/heading-has-content": "off",
    },
  },
  {
    ignores: [
      "node_modules/**",
      ".next/**",
      "out/**",
      "build/**",
      "coverage/**",
      "next-env.d.ts",
    ],
  },
];

export default eslintConfig;
