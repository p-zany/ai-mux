{
  pkgs,
  lib,
  config,
  inputs,
  ...
}:

{
  git-hooks.hooks = {
    deadnix = {
      enable = true;
      settings = {
        edit = true;
        noLambdaPatternNames = true;
      };
    };
    gofmt.enable = true;
    gotest.enable = true;
    nixfmt-rfc-style.enable = true;
    prettier = {
      enable = true;
      excludes = [
        "flake.lock"
      ];
      settings = {
        write = true;
        configPath = "./.prettierrc.yaml";
      };
    };
    statix.enable = true;
  };

  languages.go.enable = true;
}
