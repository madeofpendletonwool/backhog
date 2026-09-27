# kotlinx.serialization and Retrofit ship their own consumer rules; the
# API interface only needs its generic signatures kept.
-keepattributes Signature, InnerClasses, EnclosingMethod, *Annotation*
-keep,allowobfuscation,allowshrinking interface com.collinpendleton.backhog.api.BackhogApi
