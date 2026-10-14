# Injects FW_VERSION from version.txt so the server and firmware agree on it.
Import("env")
import os
with open(os.path.join(env.subst("$PROJECT_DIR"), "version.txt")) as f:
    v = f.read().strip()
env.Append(CPPDEFINES=[("FW_VERSION", '\\"%s\\"' % v)])
