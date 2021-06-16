/* https://github.com/api7/lua-resty-radixtree
 *
 * Copyright 2019-2020 Shenzhen ZhiLiu Technology Co., Ltd.
 * https://www.apiseven.com
 *
 * See the NOTICE file distributed with
 * this work for additional information regarding copyright ownership.
 * The owner licenses this file to You under the Apache License, Version 2.0;
 * you may not use this file except in compliance with
 * the License. You may obtain a copy of the License at
 *
 *    http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

#ifndef CRAX_EASY_RAX_H
#define CRAX_EASY_RAX_H

#ifdef __cplusplus
extern "C"
{
#endif

#include <stdint.h>
#include <stdio.h>
#include <ctype.h>
#include "rax.h"

#ifdef BUILDING_SO
#ifndef __APPLE__
#define LSH_EXPORT __attribute__((visibility("protected")))
#else
/* OSX does not support protect-visibility */
#define LSH_EXPORT __attribute__((visibility("default")))
#endif
#else
#define LSH_EXPORT
#endif

    /* **************************************************************************
 *
 *              Export Functions
 *
 * **************************************************************************
 */

    void *radix_tree_new();
    int radix_tree_destroy(void *t);
    int radix_tree_insert(void *t, const unsigned char *buf, size_t len,
                          void *data);
    void *radix_tree_find(void *t, const unsigned char *buf, size_t len);
    void *radix_tree_search(void *t, void *it, const unsigned char *buf, size_t len);
    void *radix_tree_pcre(void *it, const unsigned char *buf, size_t len);
    int radix_tree_next(void *it);
    int radix_tree_stop(void *it);

    unsigned char *radix_tree_key(void *it);
    size_t radix_tree_key_len(void *it);
    void *radix_tree_value(void *it);

    uint64_t radix_nodes_num(void *t);
    uint64_t radix_nodes_size(void *t);

    void *radix_tree_new_it(void *t);
    void radix_show(void *t);

#ifdef __cplusplus
}
#endif

#endif
